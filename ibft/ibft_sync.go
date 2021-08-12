package ibft

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	historySync "github.com/bloxapp/ssv/ibft/sync/history"
	syncReqHandler "github.com/bloxapp/ssv/ibft/sync/incoming"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
	"time"
)

// processSyncQueueMessages is listen for all the ibft historySync msg's and process them
func (i *ibftImpl) processSyncQueueMessages() {
	go func() {
		for {
			if syncMsg := i.msgQueue.PopMessage(msgqueue.SyncIndexKey(i.Identifier)); syncMsg != nil {
				i.ProcessSyncMessage(&network.SyncChanObj{
					Msg:    syncMsg.SyncMessage,
					Stream: syncMsg.Stream,
				})
			}
			time.Sleep(time.Millisecond * 100)
		}
	}()
}

func (i *ibftImpl) ProcessSyncMessage(msg *network.SyncChanObj) {
	var lastSentMsg *proto.SignedMessage
	if i.currentInstance != nil {
		lastSentMsg = i.currentInstance.LastBroadcastedMsg()
	}

	s := syncReqHandler.New(i.logger, i.Identifier, i.network, i.ibftStorage, lastSentMsg)
	go s.Process(msg)
}

// SyncIBFT will fetch best known decided message (highest sequence) from the network and historySync to it.
func (i *ibftImpl) SyncIBFT() {
	i.logger.Info("syncing iBFT..")

	// stop current instance and return any waiting chan.
	if i.currentInstance != nil {
		i.currentInstance.Stop()
	}

	// historySync
	s := historySync.New(
		i.logger,
		i.ValidatorShare.PublicKey.Serialize(),
		i.GetIdentifier(),
		i.network,
		i.ibftStorage,
		i.validateDecidedMsg,
		i.validateLastChangeRoundMsg,
	)
	err := s.Start()
	if err != nil {
		i.logger.Fatal("history historySync failed", zap.Error(err))
	}
}

func (i *ibftImpl) validateLastChangeRoundMsg(msg *proto.SignedMessage) error {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.ValidateLambdas(i.Identifier),
		auth.AuthorizeMsg(i.ValidatorShare),
		auth.MsgTypeCheck(proto.RoundState_ChangeRound),
	).Run(msg)
}
