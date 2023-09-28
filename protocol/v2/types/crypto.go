package types

import (
	"encoding/hex"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// VerifyByOperators verifies signature by the provided operators
// This is a copy of a function with the same name from the spec, except for it's use of
// DeserializeBLSPublicKey function and bounded.CGO
//
// TODO: rethink this function and consider moving/refactoring it.
func VerifyByOperators(s spectypes.Signature, data spectypes.MessageSignature, domain spectypes.DomainType, sigType spectypes.SignatureType, operators []*spectypes.Operator) error {
	// decode sig
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	// find operators
	pks := make([]bls.PublicKey, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pk, err := DeserializeBLSPublicKey(n.GetPublicKey())
				if err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}

				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// compute root
	computedRoot, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	// verify
	// if res := sign.FastAggregateVerify(pks, computedRoot[:]); !res {
	// 	return errors.New("failed to verify signature")
	// }
	if res := Verifier.AggregateVerify(sign, pks, computedRoot); !res {
		return SingleVerifyByOperators(sign, data, domain, sigType, operators)
	}
	return nil
}

func SingleVerifyByOperators(sign *bls.Sign, data spectypes.MessageSignature, domain spectypes.DomainType, sigType spectypes.SignatureType, operators []*spectypes.Operator) error {
	// find operators
	pks := make([]bls.PublicKey, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pk, err := DeserializeBLSPublicKey(n.GetPublicKey())
				if err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}

				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// compute root
	computedRoot, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	// verify
	if res := sign.FastAggregateVerify(pks, computedRoot[:]); !res {
		return errors.New("failed to verify signature")
	}
	return nil
}

func ReconstructSignature(ps *specssv.PartialSigContainer, root [32]byte, validatorPubKey []byte) ([]byte, error) {
	// Reconstruct signatures
	signature, err := spectypes.ReconstructSignatures(ps.Signatures[rootHex(root)])
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct signatures")
	}
	if err := VerifyReconstructedSignature(signature, validatorPubKey, root); err != nil {
		return nil, errors.Wrap(err, "failed to verify reconstruct signature")
	}
	return signature.Serialize(), nil
}

func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey []byte, root [32]byte) error {
	pk, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return errors.Wrap(err, "could not deserialize validator pk")
	}

	// verify reconstructed sig
	if res := Verifier.AggregateVerify(sig, []bls.PublicKey{pk}, root); !res {
		return errors.New("could not reconstruct a valid signature")
	}
	return nil
}

func rootHex(r [32]byte) string {
	return hex.EncodeToString(r[:])
}
