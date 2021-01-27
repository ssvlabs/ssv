package ibft

import (
	"bytes"
	"errors"

	"github.com/bloxapp/ssv/ibft/types"
)

// ValidateLambdas valdiates current and previous lambdas
func (i *Instance) ValidateLambdas() types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Lambda, i.state.Lambda) {
			return errors.New("message lambda does not equal state lambda")
		}
		if !bytes.Equal(signedMessage.Message.PreviousLambda, i.state.PreviousLambda) {
			return errors.New("message previous lambda does not equal state previous lambda")
		}
		return nil
	}
}

func (i *Instance) ValidateRound() types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		if i.state.Round != signedMessage.Message.Round {
			return errors.New("message round does not equal state round")
		}
		return nil
	}
}

func (i *Instance) AuthMsg() types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		pks, err := i.params.PubKeysById([]uint64{signedMessage.IbftId})
		if err != nil {
			return err
		}
		if len(pks) != 1 {
			return errors.New("could not find public key")
		}

		res, err := signedMessage.VerifySig(pks[0])
		if err != nil {
			return err
		}
		if !res {
			return errors.New("could not verify message signature")
		}
		return nil
	}
}

func MsgTypeCheck(expected types.RoundState) types.PipelineFunc {
	return func(signedMessage *types.SignedMessage) error {
		if signedMessage.Message.Type != expected {
			return errors.New("message type is wrong")
		}
		return nil
	}
}
