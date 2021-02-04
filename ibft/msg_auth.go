package ibft

import (
	"bytes"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/pkg/errors"
)

// ValidateLambdas validates current and previous lambdas
func (i *Instance) ValidateLambdas() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Lambda, i.State.Lambda) {
			return errors.Errorf("message lambda (%s) does not equal State lambda (%s)", string(signedMessage.Message.Lambda), string(i.State.Lambda))
		}
		if !bytes.Equal(signedMessage.Message.PreviousLambda, i.State.PreviousLambda) {
			return errors.New("message previous lambda does not equal State previous lambda")
		}
		return nil
	}
}

func (i *Instance) ValidateRound() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if i.State.Round != signedMessage.Message.Round {
			return errors.Errorf("message round (%d) does not equal State round (%d)", signedMessage.Message.Round, i.State.Round)
		}
		return nil
	}
}

func (i *Instance) AuthMsg() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
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

func MsgTypeCheck(expected proto.RoundState) PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if signedMessage.Message.Type != expected {
			return errors.New("message type is wrong")
		}
		return nil
	}
}
