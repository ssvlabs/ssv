package sync

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

// HistorySync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type ReqHandler struct {
	network network.Network
	storage collections.Iibft
	logger  *zap.Logger
}

// NewReqHandler returns a new instance of ReqHandler
func NewReqHandler(logger *zap.Logger, network network.Network, storage collections.Iibft) *ReqHandler {
	return &ReqHandler{logger: logger, network: network, storage: storage}
}

func (s *ReqHandler) Process(msg *network.SyncChanObj) {
	switch msg.Msg.Type {
	case network.Sync_GetHighestType:
		s.handleGetHighestReq(msg.Stream)
	default:
		s.logger.Error("sync req handler received un-supported type", zap.Uint64("received type", uint64(msg.Msg.Type)))
	}
}

func (s *ReqHandler) handleGetHighestReq(stream network.SyncStream) {
	highest, err := s.storage.GetHighestDecidedInstance([]byte{}) // TODO - TBD
	if err != nil {
		s.logger.Error("failed to get highest decided from db", zap.Error(err))
	}
	res := &network.SyncMessage{
		SignedMessages: []*proto.SignedMessage{highest},
		Type:           network.Sync_GetHighestType,
	}

	if err := s.network.RespondToHighestDecidedInstance(stream, res); err != nil {
		s.logger.Error("failed to send highest decided response", zap.Error(err))
	}
}
