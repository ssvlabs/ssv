package kms

import (
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	dkgkms "github.com/ssvlabs/ssv-dkg/pkgs/kms"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// Key is a dKMS key this operator holds a share of.
type Key struct {
	ID        [32]byte
	IDHex     string
	File      *dkgkms.KeyFile
	Scheme    dkgkms.Scheme
	Share     dkgkms.Share
	PublicKey []byte
	Address   common.Address
	Owner     common.Address
	// Threshold is the number of signers required.
	Threshold int
	Committee []spectypes.OperatorID
	// OperatorKeys maps committee members to their base64-PEM RSA public keys.
	OperatorKeys map[spectypes.OperatorID]string
}

// KeyInfo is the public description of a key.
type KeyInfo struct {
	KeyID     string   `json:"key_id"`
	Scheme    string   `json:"scheme"`
	PublicKey string   `json:"public_key"`
	Address   string   `json:"address"`
	Owner     string   `json:"owner"`
	Threshold int      `json:"threshold"`
	Operators []uint64 `json:"operators"`
	Height    uint64   `json:"height"`
	// Presignatures is how many presignatures this operator holds, by signer set.
	Presignatures map[string]int `json:"presignatures"`
}

// openKey checks a key file and opens this operator's share of it.
func openKey(kf *dkgkms.KeyFile, self spectypes.OperatorID, decrypt func([]byte) ([]byte, error), operatorKey func(spectypes.OperatorID) (string, error)) (*Key, error) {
	if err := kf.Verify(); err != nil {
		return nil, err
	}
	scheme, err := dkgkms.SchemeByID(kf.Scheme)
	if err != nil {
		return nil, err
	}
	keyID, err := dkgkms.ParseKeyID(kf.KeyID)
	if err != nil {
		return nil, err
	}
	k := &Key{
		ID:           keyID,
		IDHex:        keyID.String(),
		File:         kf,
		Scheme:       scheme,
		Address:      common.HexToAddress(kf.Address),
		Owner:        common.HexToAddress(kf.Owner),
		Threshold:    kf.Threshold,
		OperatorKeys: map[spectypes.OperatorID]string{},
	}
	if k.PublicKey, err = dkgkms.DecodeHex(kf.PublicKey); err != nil {
		return nil, err
	}
	// The committee in the key file must be the operators this node knows (registry keys).
	for _, op := range kf.Operators {
		registered, err := operatorKey(op.ID)
		if err != nil {
			return nil, fmt.Errorf("operator %d: %w", op.ID, err)
		}
		a, err := dkgkms.ParseRSAPublicKey(registered)
		if err != nil {
			return nil, fmt.Errorf("operator %d registry key: %w", op.ID, err)
		}
		b, err := dkgkms.ParseRSAPublicKey(op.PubKey)
		if err != nil || !a.Equal(b) {
			return nil, fmt.Errorf("operator %d key in the key file is not its registered key", op.ID)
		}
		k.Committee = append(k.Committee, op.ID)
		k.OperatorKeys[op.ID] = registered
	}
	slices.Sort(k.Committee)
	if !slices.Contains(k.Committee, self) {
		return nil, fmt.Errorf("operator %d is not in this key's committee", self)
	}

	var entry *dkgkms.ShareEntry
	for i := range kf.Shares {
		if kf.Shares[i].OperatorID == self {
			entry = &kf.Shares[i]
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("no share for operator %d", self)
	}
	plain, err := dkgkms.Open(decrypt, entry.EncryptedShare, dkgkms.ShareAAD(keyID, self))
	if err != nil {
		return nil, fmt.Errorf("decrypt share: %w", err)
	}
	if k.Share, err = scheme.UnmarshalShare(plain); err != nil {
		return nil, err
	}
	pub, err := k.Share.PublicKey()
	if err != nil {
		return nil, err
	}
	if common.Bytes2Hex(pub) != common.Bytes2Hex(k.PublicKey) {
		return nil, fmt.Errorf("share is for a different key")
	}
	ids, err := k.Share.OperatorIDs()
	if err != nil {
		return nil, err
	}
	if !slices.Equal(ids, k.Committee) {
		return nil, fmt.Errorf("share committee %v does not match key committee %v", ids, k.Committee)
	}
	return k, nil
}

// Info returns the key's public description.
func (k *Key) Info() KeyInfo {
	return KeyInfo{
		KeyID:     k.IDHex,
		Scheme:    string(k.Scheme.ID()),
		PublicKey: k.File.PublicKey,
		Address:   k.Address.Hex(),
		Owner:     k.Owner.Hex(),
		Threshold: k.Threshold,
		Operators: k.Committee,
	}
}

// committeeMember is the QBFT view of the key's committee.
func (k *Key) committeeMember(self spectypes.OperatorID, domain spectypes.DomainType) (*spectypes.CommitteeMember, error) {
	member := &spectypes.CommitteeMember{
		OperatorID:  self,
		CommitteeID: ssvtypes.ComputeCommitteeID(k.Committee),
		FaultyNodes: ssvtypes.ComputeF(uint64(len(k.Committee))),
		DomainType:  domain,
	}
	for _, id := range k.Committee {
		pem, err := base64.StdEncoding.DecodeString(k.OperatorKeys[id])
		if err != nil {
			return nil, err
		}
		member.Committee = append(member.Committee, &spectypes.Operator{OperatorID: id, SSVOperatorPubKey: pem})
		if id == self {
			member.SSVOperatorPubKey = pem
		}
	}
	return member, nil
}

// signerSets returns every set of Threshold operators of the committee that includes self, in a
// stable order.
func (k *Key) signerSets(self spectypes.OperatorID) [][]uint64 {
	var out [][]uint64
	var rec func(start int, cur []uint64)
	rec = func(start int, cur []uint64) {
		if len(cur) == k.Threshold {
			if slices.Contains(cur, self) {
				out = append(out, slices.Clone(cur))
			}
			return
		}
		for i := start; i < len(k.Committee); i++ {
			rec(i+1, append(cur, k.Committee[i]))
		}
	}
	rec(0, nil)
	return out
}

func (k *Key) logger(l *zap.Logger) *zap.Logger {
	return l.With(zap.String("kms_key", k.IDHex), zap.String("kms_address", k.Address.Hex()))
}

// slotOf is the "slot" QBFT and the round timer see for a height: dKMS heights are request
// sequence numbers, which the round-relative KMS role never maps to wall-clock time.
func slotOf(height uint64) phase0.Slot { return phase0.Slot(height) }
