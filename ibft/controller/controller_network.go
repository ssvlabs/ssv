package controller

import (
	"bytes"
	"context"
	"time"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
)

var (
	waitMinPeersIntervalStart = 1 * time.Second
	waitMinPeersIntervalEnd   = 64 * time.Second
)

// waitForMinPeerOnInit is called once during IBFTController init
func (i *Controller) waitForMinPeerOnInit(minPeerCount int) {
	// warmup to avoid network errors
	time.Sleep(500 * time.Millisecond)

	if err := i.waitForMinPeers(minPeerCount, false); err != nil {
		i.logger.Warn("could not find min peers")
	}
}

// waitForMinPeers will wait until enough peers joined the topic
// it runs in an exponent interval: 1s > 2s > 4s > ... 64s > 1s > 2s > ...
func (i *Controller) waitForMinPeers(minPeerCount int, stopAtLimit bool) error {
	ctx := commons.WaitMinPeersCtx{
		Ctx:    context.Background(),
		Logger: i.logger,
		Net:    i.network,
	}
	return commons.WaitForMinPeers(ctx, i.ValidatorShare.PublicKey.Serialize(), minPeerCount,
		waitMinPeersIntervalStart, waitMinPeersIntervalEnd, stopAtLimit)
}

func (i *Controller) listenToNetworkMessages() {
	msgChan, done := i.network.ReceivedMsgChan()
	go func() {
		defer done()
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

func (i *Controller) listenToSyncMessages() {
	syncChan, done := i.network.ReceivedSyncMsgChan()
	go func() {
		defer done()
		ctx, cancel := context.WithCancel(i.ctx)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-syncChan:
				if msg.Msg != nil && i.equalIdentifier(msg.Msg.Lambda) {
					i.msgQueue.AddMessage(&network.Message{
						SyncMessage: msg.Msg,
						StreamID:    msg.StreamID,
						Type:        network.NetworkMsg_SyncType,
					})
				}
			}
		}
	}()
}

func (i *Controller) equalIdentifier(toCheck []byte) bool {
	return bytes.Equal(toCheck, i.Identifier)
}
