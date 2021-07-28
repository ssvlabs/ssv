package ibft

import (
	"bytes"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/tasks"
	"go.uber.org/zap"
	"time"
)

// waitForMinPeerCount will wait until enough peers joined the topic
// it runs in an exponent interval: 1s > 2s > 4s > ... 64s > 1s > 2s > ...
func (i *ibftImpl) waitForMinPeerCount(minPeerCount int) {
	intervalLimit := 65 * time.Second
	tasks.ExecWithInterval(func(lastTick time.Duration) (bool, bool) {
		peers, err := i.network.AllPeers(i.ValidatorShare.PublicKey.Serialize())
		if err != nil {
			i.logger.Error("failed fetching peers", zap.Error(err))
			// continue without increasing interval
			return false, true
		}

		if len(peers) >= minPeerCount {
			// stopped interval if we found enough peers
			i.logger.Info("found enough peers",
				zap.Int("current peer count", len(peers)))
			return true, false
		}
		i.logger.Info("waiting for min peer count",
			zap.Int("current peer count", len(peers)),
			zap.Int64("last interval ms", lastTick.Milliseconds()))
		return false, false
	}, time.Second, intervalLimit)
}

func (i *ibftImpl) listenToNetworkMessages() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for msg := range msgChan {
			if msg.Message != nil && i.equalIdentifier(msg.Message.Lambda) {
				i.msgQueue.AddMessage(&network.Message{
					SignedMessage: msg,
					Type:          network.NetworkMsg_IBFTType,
				})
			}
		}
	}()
}

func (i *ibftImpl) listenToNetworkDecidedMessages() {
	decidedChan := i.network.ReceivedDecidedChan()
	go func() {
		for msg := range decidedChan {
			if msg.Message != nil && i.equalIdentifier(msg.Message.Lambda) {
				i.msgQueue.AddMessage(&network.Message{
					SignedMessage: msg,
					Type:          network.NetworkMsg_DecidedType,
				})
			}
		}
	}()
}

func (i *ibftImpl) listenToSyncMessages() {
	syncChan := i.network.ReceivedSyncMsgChan()
	go func() {
		for msg := range syncChan {
			if msg.Msg != nil && i.equalIdentifier(msg.Msg.Lambda) {
				i.msgQueue.AddMessage(&network.Message{
					SyncMessage: msg.Msg,
					Stream:      msg.Stream,
					Type:        network.NetworkMsg_SyncType,
				})
			}
		}
	}()
}

func (i *ibftImpl) equalIdentifier(toCheck []byte) bool {
	return bytes.Equal(toCheck, i.Identifier)
}
