package types

import (
	"errors"
	"fmt"

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
