package types

import (
	"crypto/sha256"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

type SignatureDomain []byte
type Signature []byte

// VerifyByOperators verifies signature by the provided operators
func (s Signature) VerifyByOperators(data MessageSignature, domain DomainType, sigType SignatureType, operators []*Operator) error {
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
	return s.VerifyMultiPubKey(data, domain, sigType, pks)
}

func (s Signature) VerifyMultiPubKey(data Root, domain DomainType, sigType SignatureType, pks [][]byte) error {
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

	return s.Verify(data, domain, sigType, aggPK.Serialize())
}

func (s Signature) Verify(data Root, domain DomainType, sigType SignatureType, pkByts []byte) error {
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	pk := &bls.PublicKey{}
	if err := pk.Deserialize(pkByts); err != nil {
		return errors.Wrap(err, "failed to deserialize public key")
	}

	computedRoot, err := ComputeSigningRoot(data, ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}
	if res := sign.VerifyByte(pk, computedRoot); !res {
		return errors.New("failed to verify signature")
	}
	return nil
}

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

func ComputeSigningRoot(data Root, domain SignatureDomain) ([]byte, error) {
	dataRoot, err := data.GetRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not get root from Root")
	}

	ret := sha256.Sum256(append(dataRoot, domain...))
	return ret[:], nil
}

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
