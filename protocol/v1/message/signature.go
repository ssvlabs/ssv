package message

import (
	"crypto/sha256"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// DomainType is a unique identifier for signatures, 2 identical pieces of data signed with different domains will result in different sigs
type DomainType []byte

var (
	// PrimusTestnet is the domain type for the testnet
	PrimusTestnet = DomainType("primus_testnet")
)

// SignatureType is the type of the signature
type SignatureType []byte

var (
	// QBFTSigType is the type for QBFT signatures
	QBFTSigType = []byte{1, 0, 0, 0}
	// PostConsensusSigType is the type for post consensus signatures
	PostConsensusSigType = []byte{2, 0, 0, 0}
)

// Root holds an interface to access the root for signing/verification
type Root interface {
	// GetRoot returns the root used for signing and verification
	GetRoot(forkVersion string) ([]byte, error)
}

// MsgSignature includes all functions relevant for a signed message (QBFT message, post consensus msg, etc)
type MsgSignature interface {
	Root
	GetSignature() Signature
	GetSigners() []OperatorID
	// MatchedSigners returns true if the provided signer ids are equal to GetSignerIds() without order significance
	MatchedSigners(ids []OperatorID) bool
	// Aggregate will aggregate the signed message if possible (unique signers, same digest, valid)
	Aggregate(signedMsg ...MsgSignature) error // value should depend on implementation
}

// SignatureDomain represents signature domain bytes
type SignatureDomain []byte

// Signature represents signature bytes
type Signature []byte

// VerifyByOperators verifies signature by the provided operators
func (s Signature) VerifyByOperators(data MsgSignature, domain DomainType, sigType SignatureType, operators []*Operator, forkVersion string) error {
	if data.GetSignature() == nil || len(data.GetSignature()) == 0 {
		return errors.New("message signature is invalid")
	}

	// signer uniqueness
	err := verifyUniqueSigners(data.GetSigners())
	if err != nil {
		return errors.Wrap(err, "signers are not unique")
	}

	pks := make([][]byte, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pks = append(pks, n.GetPublicKey())
				found = true
			}
		}
		if !found {
			return errors.New("signer not found in operators")
		}
	}
	return s.VerifyMultiPubKey(data, domain, sigType, pks, forkVersion)
}

// VerifyMultiPubKey verifies the signature for multiple public keys
func (s Signature) VerifyMultiPubKey(data Root, domain DomainType, sigType SignatureType, pks [][]byte, forkVersion string) error {
	var aggPK *bls.PublicKey
	for _, pkByts := range pks {
		pk := &bls.PublicKey{}
		if err := pk.Deserialize(pkByts); err != nil {
			return errors.Wrap(err, "failed to deserialize public key")
		}

		if aggPK == nil {
			aggPK = pk
		} else {
			aggPK.Add(pk)
		}
	}

	if aggPK == nil {
		return errors.New("no public keys found")
	}
	return s.Verify(data, domain, sigType, aggPK.Serialize(), forkVersion)
}

// Verify verifies the signature for the given public key
func (s Signature) Verify(data Root, domain DomainType, sigType SignatureType, pkByts []byte, forkVersion string) error {
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(pkByts); err != nil {
		return errors.Wrap(err, "failed to deserialize public key")
	}

	//computedRoot, err := ComputeSigningRoot(data, ComputeSignatureDomain(domain, sigType)) //TODO this code is from the new spec. need to align the signing too in order to use domain
	computedRoot, err := data.GetRoot(forkVersion)
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}
	if res := sign.VerifyByte(pk, computedRoot); !res {
		return errors.New("failed to verify signature")
	}
	return nil
}

// Aggregate aggregates other signatures
func (s Signature) Aggregate(other Signature) (Signature, error) {
	s1 := &bls.Sign{}
	if err := s1.Deserialize(s); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize signature")
	}

	s2 := &bls.Sign{}
	if err := s2.Deserialize(other); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize signature")
	}

	s1.Add(s2)
	return s1.Serialize(), nil
}

// ComputeSigningRoot computes the signing root
func ComputeSigningRoot(data Root, domain SignatureDomain) ([]byte, error) {
	dataRoot, err := data.GetRoot("")
	if err != nil {
		return nil, errors.Wrap(err, "could not get root from Root")
	}

	ret := sha256.Sum256(append(dataRoot, domain...))
	return ret[:], nil
}

// ComputeSignatureDomain computes the signature domain according to domain+signature types
func ComputeSignatureDomain(domain DomainType, sigType SignatureType) SignatureDomain {
	return SignatureDomain(append(domain, sigType...))
}

// ReconstructSignatures receives a map of user indexes and serialized bls.Sign.
// It then reconstructs the original threshold signature using lagrange interpolation
func ReconstructSignatures(signatures map[OperatorID][]byte) (*bls.Sign, error) {
	reconstructedSig := bls.Sign{}

	idVec := make([]bls.ID, 0)
	sigVec := make([]bls.Sign, 0)

	for index, signature := range signatures {
		blsID := bls.ID{}
		err := blsID.SetDecString(fmt.Sprintf("%d", index))
		if err != nil {
			return nil, err
		}

		idVec = append(idVec, blsID)
		blsSig := bls.Sign{}

		err = blsSig.Deserialize(signature)
		if err != nil {
			return nil, err
		}

		sigVec = append(sigVec, blsSig)
	}
	err := reconstructedSig.Recover(sigVec, idVec)
	return &reconstructedSig, err
}

// VerifyReconstructedSignature verifies the reconstructed signature
func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey, root []byte) error {
	pk := &bls.PublicKey{}
	if err := pk.Deserialize(validatorPubKey); err != nil {
		return errors.Wrap(err, "could not deserialize validator pk")
	}

	// verify reconstructed sig
	if res := sig.VerifyByte(pk, root); !res {
		return errors.New("could not reconstruct a valid signature")
	}
	return nil
}

func verifyUniqueSigners(singerIds []OperatorID) error {
	unique := map[OperatorID]bool{}
	for _, signer := range singerIds {
		if _, found := unique[signer]; !found {
			unique[signer] = true
		} else {
			return errors.New("signers are not unique")
		}
	}
	return nil
}
