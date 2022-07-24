package commons

import (
	"crypto/rsa"
	"encoding/hex"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

type testKeyManager struct {
	lock sync.Locker
	keys map[string]*bls.SecretKey
}

// NewTestKeyManager creates a new signer for tests
func NewTestKeyManager() spectypes.KeyManager {
	return &testKeyManager{&sync.Mutex{}, make(map[string]*bls.SecretKey)}
}

func (km *testKeyManager) IsAttestationSlashable(data *spec.AttestationData) error {
	panic("implement me")
}

func (km *testKeyManager) SignRandaoReveal(epoch spec.Epoch, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) IsBeaconBlockSlashable(block *altair.BeaconBlock) error {
	panic("implement me")
}

func (km *testKeyManager) SignBeaconBlock(block *altair.BeaconBlock, duty *spectypes.Duty, pk []byte) (*altair.SignedBeaconBlock, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignSlotWithSelectionProof(slot spec.Slot, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignAggregateAndProof(msg *spec.AggregateAndProof, duty *spectypes.Duty, pk []byte) (*spec.SignedAggregateAndProof, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignSyncCommitteeBlockRoot(slot spec.Slot, root spec.Root, validatorIndex spec.ValidatorIndex, pk []byte) (*altair.SyncCommitteeMessage, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignContributionProof(slot spec.Slot, index uint64, pk []byte) (spectypes.Signature, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignContribution(contribution *altair.ContributionAndProof, pk []byte) (*altair.SignedContributionAndProof, []byte, error) {
	panic("implement me")
}

func (km *testKeyManager) Decrypt(pk *rsa.PublicKey, cipher []byte) ([]byte, error) {
	panic("implement me")
}

func (km *testKeyManager) Encrypt(pk *rsa.PublicKey, data []byte) ([]byte, error) {
	panic("implement me")
}

func (km *testKeyManager) SignRoot(data spectypes.Root, sigType spectypes.SignatureType, pk []byte) (spectypes.Signature, error) {
	if k, found := km.keys[hex.EncodeToString(pk)]; found {
		computedRoot, err := spectypes.ComputeSigningRoot(data, nil) // TODO(olegshmuelov) need to use sigType
		if err != nil {
			return nil, errors.Wrap(err, "could not sign root")
		}

		return k.SignByte(computedRoot).Serialize(), nil
	}
	return nil, errors.New("pk not found")
}

func (km *testKeyManager) AddShare(shareKey *bls.SecretKey) error {
	km.lock.Lock()
	defer km.lock.Unlock()

	if km.getKey(shareKey.GetPublicKey()) == nil {
		km.keys[shareKey.GetPublicKey().SerializeToHexStr()] = shareKey
	}
	return nil
}

func (km *testKeyManager) RemoveShare(pubKey string) error {
	panic("implement me")
}

func (km *testKeyManager) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testKeyManager) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}
