package ibft

import (
	"bytes"
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"time"
)

var (
	waitMinPeersIntervalStart = 1 * time.Second
	waitMinPeersIntervalEnd   = 64 * time.Second
)

// waitForMinPeerOnInit is called once during IBFT init
func (i *ibftImpl) waitForMinPeerOnInit(minPeerCount int) {
	// warmup to avoid network errors
	time.Sleep(500 * time.Millisecond)

	if err := i.waitForMinPeers(minPeerCount, false); err != nil {
		i.logger.Warn("could not find min peers")
	}
}

// waitForMinPeers will wait until enough peers joined the topic
// it runs in an exponent interval: 1s > 2s > 4s > ... 64s > 1s > 2s > ...
func (i *ibftImpl) waitForMinPeers(minPeerCount int, stopAtLimit bool) error {
	ctx := commons.WaitMinPeersCtx{
		Ctx:    context.Background(),
		Logger: i.logger,
		Net:    i.network,
	}
	return commons.WaitForMinPeers(ctx, i.ValidatorShare.PublicKey.Serialize(), minPeerCount,
		waitMinPeersIntervalStart, waitMinPeersIntervalEnd, stopAtLimit)
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
