package p2p

import (
	"go.uber.org/zap"
)

const (
	mainTopicName = "main"
)

// SubscribeToMainTopic subscribes to main topic
func (n *p2pNetwork) SubscribeToMainTopic() error {
	cn, err := n.topicManager.Subscribe(mainTopicName)
	if err != nil {
		return err
	}

	go func() {
		for n.ctx.Err() == nil {
			select {
			case msg := <-cn:
				if msg == nil {
					continue
				}
				n.trace("received raw network msg", zap.ByteString("network.Message bytes", msg.Data))
				cm, err := n.fork.DecodeNetworkMsg(msg.Data)
				if err != nil {
					n.logger.Error("failed to un-marshal message", zap.Error(err))
					continue
				}
				if n.reportLastMsg && len(msg.ReceivedFrom) > 0 {
					reportLastMsg(msg.ReceivedFrom.String())
				}
				n.propagateSignedMsg(cm)
			default:
				return
			}
		}
	}()

	return nil
}
