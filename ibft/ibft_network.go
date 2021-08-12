package ibft

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"time"
)

func (i *ibftImpl) waitForMinPeerCount(minPeerCount int) {
	// warmup to avoid network errors
	time.Sleep(500 * time.Millisecond)

	if err := i.waitForMinPeers(minPeerCount, false); err != nil {
		i.logger.Warn("could not find min peers")
	}
}

// waitForMinPeers will wait until enough peers joined the topic
// it runs in an exponent interval: 1s > 2s > 4s > ... 64s > 1s > 2s > ...
func (i *ibftImpl) waitForMinPeers(minPeerCount int, stopAtLimit bool) error {
	start := 1 * time.Second
	limit := 64 * time.Second
	interval := start
	for {
		ok, n := i.haveMinPeers(minPeerCount)
		if ok {
			i.logger.Info("found enough peers",
				zap.Int("current peer count", n))
			break
		}
		i.logger.Info("waiting for min peers",
			zap.Int("current peer count", n))

		time.Sleep(interval)

		interval *= 2
		if stopAtLimit && interval == limit {
			return errors.New("could not find peers")
		}
		interval %= limit
		if interval == 0 {
			interval = start
		}
	}
	return nil
}

// haveMinPeers checks that there are at least <count> connected peers
func (i *ibftImpl) haveMinPeers(count int) (bool, int) {
	peers, err := i.network.AllPeers(i.ValidatorShare.PublicKey.Serialize())
	if err != nil {
		i.logger.Error("failed fetching peers", zap.Error(err))
		return false, 0
	}
	n := len(peers)
	if len(peers) >= count {
		return true, n
	}
	return false, n
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
