package ibft

import (
	"bytes"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/pkg/errors"
)

// WaitForStage waits until the current instance has the same state with signed message
func (i *Instance) WaitForStage() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if i.State.Stage+1 >= signedMessage.Message.Type {
			return nil
		}
		i.logger.Info("got non-broadcasted message",
			zap.String("message_stage", signedMessage.Message.Type.String()),
			zap.String("current_stage", i.State.Stage.String()))

		ch := i.GetStageChan()
		dif := signedMessage.Message.Type - i.State.Stage + 1
		for j := 0; j < int(dif); j++ {
			st := <-ch
			i.logger.Info("got changed state", zap.String("state", st.String()))
			if st >= signedMessage.Message.Type || i.State.Stage+1 >= signedMessage.Message.Type {
				break
			}
		}

		return nil
	}
}

// ValidateLambdas validates current and previous lambdas
func (i *Instance) ValidateLambdas() PipelineFunc {
	return func(signedMessage *proto.SignedMessage) error {
		if !bytes.Equal(signedMessage.Message.Lambda, i.State.Lambda) {
			return errors.Errorf("message Lambda (%s) does not equal State Lambda (%s)", string(signedMessage.Message.Lambda), string(i.State.Lambda))
		}
		if !bytes.Equal(signedMessage.Message.PreviousLambda, i.State.PreviousLambda) {
			return errors.New("message previous Lambda does not equal State previous Lambda")
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
		pks, err := i.params.PubKeysById(signedMessage.SignerIds)
		if err != nil {
			return err
		}
		if len(pks) == 0 {
			return errors.New("could not find public key")
		}

		var foundVerified bool
		for _, pk := range pks {
			res, err := signedMessage.VerifySig(pk)
			if err != nil {
				return err
			}

			if res {
				foundVerified = true
				break
			}
		}
		if !foundVerified {
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
