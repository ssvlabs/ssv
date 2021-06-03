package ibft

import (
	"bytes"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"strings"
	"time"
)

func (i *ibftImpl) waitForMinPeerCount(minPeerCount int) {
	for {
		time.Sleep(time.Second)

		peers, err := i.network.AllPeers(i.ValidatorShare.PublicKey.Serialize())
		if err != nil {
			i.logger.Error("failed fetching peers", zap.Error(err))
			continue
		}

		i.logger.Debug("waiting for min peer count", zap.Int("current peer count", len(peers)))
		if len(peers) >= minPeerCount {
			break
		}
	}
}

func (i *ibftImpl) listenToNetworkMessages() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for msg := range msgChan {
			i.msgQueue.AddMessage(&network.Message{
				Lambda:        msg.Message.Lambda,
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
	}()

	// decided messages
	decidedChan := i.network.ReceivedDecidedChan()
	go func() {
		for msg := range decidedChan {
			if i.validateMsg(msg) {
				i.ProcessDecidedMessage(msg)
			}
		}
	}()
}

func (i *ibftImpl) listenToSyncMessages() {
	// sync messages
	syncChan := i.network.ReceivedSyncMsgChan()
	go func() {
		for msg := range syncChan { // TODO add validator pubkey validation
			i.ProcessSyncMessage(msg) // TODO verify duty type
		}
	}()
}

// validateMsg for each ibft controller by pubkey, role
func (i *ibftImpl) validateMsg(msg *proto.SignedMessage) bool {
	valid := bytes.Equal(i.ValidatorShare.PublicKey.Serialize(), msg.Message.ValidatorPk)
	valid = i.role.String() == strings.Split(string(msg.Message.Lambda), "_")[1]
	return valid
}
