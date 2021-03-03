package ibft

import (
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// WaitForStage waits until the current instance has the same state with signed message
func (i *Instance) WaitForStage() pipeline.Pipeline {
	return pipeline.WrapFunc(func(signedMessage *proto.SignedMessage) error {
		if i.State.Stage+1 >= signedMessage.Message.Type {
			return nil
		}
		i.Logger.Info("got non-broadcasted message",
			zap.String("message_stage", signedMessage.Message.Type.String()),
			zap.String("current_stage", i.State.Stage.String()))

		ch := i.GetStageChan()
		dif := signedMessage.Message.Type - i.State.Stage + 1
		for j := 0; j < int(dif); j++ {
			st := <-ch
			i.Logger.Info("got changed state", zap.String("state", st.String()))
			if st >= signedMessage.Message.Type || i.State.Stage+1 >= signedMessage.Message.Type {
				break
			}
		}

		return nil
	})
}
