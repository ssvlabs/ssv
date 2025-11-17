package types

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey []byte, root [32]byte) error {
	pk, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return errors.Wrap(err, "could not deserialize validator pk")
	}

	if res := sig.VerifyByte(&pk, root[:]); !res {
		return spectypes.NewError(spectypes.ReconstructSignatureErrorCode, "could not reconstruct a valid signature")
	}
	return nil
}
