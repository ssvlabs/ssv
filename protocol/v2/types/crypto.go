package types

import (
	"errors"
	"fmt"
	"slices"

	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey []byte, root [32]byte) error {
	pk, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return fmt.Errorf("could not deserialize validator pk: %w", err)
	}

	if res := sig.VerifyByte(&pk, root[:]); !res {
		return spectypes.NewError(spectypes.ReconstructSignatureErrorCode, "could not reconstruct a valid signature")
	}
	return nil
}

// VerifyBeaconPartialSignature verifies a single operator's partial signature over root
// against that operator's share public key, looked up in committee by signer ID. It
// returns an error if the signer is not in the committee or the signature is invalid.
func VerifyBeaconPartialSignature(signer spectypes.OperatorID, signature spectypes.Signature, root [32]byte, committee []*spectypes.ShareMember) error {
	for _, member := range committee {
		if member.Signer != signer {
			continue
		}
		pk, err := DeserializeBLSPublicKey(member.SharePubKey)
		if err != nil {
			return fmt.Errorf("could not deserialize share public key: %w", err)
		}
		sig := &bls.Sign{}
		if err := sig.Deserialize(signature); err != nil {
			return fmt.Errorf("could not deserialize signature: %w", err)
		}
		if !sig.VerifyByte(&pk, root[:]) {
			return errors.New("wrong signature")
		}
		return nil
	}
	return errors.New("unknown signer")
}

// ErrInvalidSharePublicKeys is wrapped by every error ValidateSharePublicKeys returns.
var ErrInvalidSharePublicKeys = errors.New("invalid share public keys")

// ValidateSharePublicKeys checks that the share public keys are evaluations, at the operator IDs, of
// one polynomial of degree exactly quorum-1 whose free coefficient is the validator public key, so
// that every quorum of operators reconstructs signatures for the validator public key.
//
// The first quorum-1 shares form a base that, with (0, validatorPubKey), uniquely defines a polynomial
// of degree at most quorum-1. Base + each remaining share must recover the validator public key, which
// puts every share on that polynomial. The base alone must not, or the degree is below quorum-1.
func ValidateSharePublicKeys(validatorPubKey []byte, operatorIDs []uint64, sharePubKeys [][]byte) error {
	if err := validateSharePublicKeys(validatorPubKey, operatorIDs, sharePubKeys); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSharePublicKeys, err)
	}
	return nil
}

func validateSharePublicKeys(validatorPubKey []byte, operatorIDs []uint64, sharePubKeys [][]byte) error {
	if len(operatorIDs) != len(sharePubKeys) {
		return fmt.Errorf("operator count (%d) does not match share public key count (%d)", len(operatorIDs), len(sharePubKeys))
	}
	if !ValidCommitteeSize(uint64(len(operatorIDs))) {
		return fmt.Errorf("invalid committee size (%d)", len(operatorIDs))
	}

	expected, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return fmt.Errorf("failed to deserialize validator public key: %w", err)
	}

	ids := make([]bls.ID, len(operatorIDs))
	pubKeys := make([]bls.PublicKey, len(sharePubKeys))
	for i := range operatorIDs {
		if err := ids[i].SetDecString(fmt.Sprintf("%d", operatorIDs[i])); err != nil {
			return fmt.Errorf("failed to set id for operator %d: %w", operatorIDs[i], err)
		}
		// Copied to avoid the "cgo argument has Go pointer to unpinned Go pointer" panic when the
		// input points into a struct holding Go pointers (e.g. SSVShare).
		if err := pubKeys[i].Deserialize(slices.Clone(sharePubKeys[i])); err != nil {
			return fmt.Errorf("failed to deserialize share public key of operator %d: %w", operatorIDs[i], err)
		}
	}

	quorum, _ := ComputeQuorumAndPartialQuorum(uint64(len(operatorIDs)))
	baseSize := quorum - 1

	baseRecovered := &bls.PublicKey{}
	if err := baseRecovered.Recover(pubKeys[:baseSize], ids[:baseSize]); err != nil {
		return fmt.Errorf("failed to recover public key: %w", err)
	}
	if baseRecovered.IsEqual(&expected) {
		return fmt.Errorf("polynomial degree below %d: the first %d shares alone reconstruct the validator public key", baseSize, baseSize)
	}

	subIDs := make([]bls.ID, quorum)
	subPubKeys := make([]bls.PublicKey, quorum)
	copy(subIDs, ids[:baseSize])
	copy(subPubKeys, pubKeys[:baseSize])

	for j := baseSize; j < uint64(len(pubKeys)); j++ {
		subIDs[baseSize] = ids[j]
		subPubKeys[baseSize] = pubKeys[j]

		recovered := &bls.PublicKey{}
		if err := recovered.Recover(subPubKeys, subIDs); err != nil {
			return fmt.Errorf("failed to recover public key: %w", err)
		}
		if !recovered.IsEqual(&expected) {
			return fmt.Errorf("validator public key not reconstructed (first mismatch at operator %d)", operatorIDs[j])
		}
	}

	return nil
}
