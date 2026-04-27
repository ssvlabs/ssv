package types

import (
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
