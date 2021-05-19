package sync

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

// ReqHandler is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type ReqHandler struct {
	// paginationMaxSize is the max number of returned elements in a single response
	paginationMaxSize uint64
	validatorPK       []byte
	network           network.Network
	storage           collections.Iibft
	logger            *zap.Logger
}

// NewReqHandler returns a new instance of ReqHandler
func NewReqHandler(logger *zap.Logger, validatorPK []byte, network network.Network, storage collections.Iibft) *ReqHandler {
	return &ReqHandler{
		paginationMaxSize: 25, // TODO - change to be a param
		logger:            logger,
		validatorPK:       validatorPK,
		network:           network,
		storage:           storage,
	}
}

// Process takes a req and processes it
func (s *ReqHandler) Process(msg *network.SyncChanObj) {
	switch msg.Msg.Type {
	case network.Sync_GetHighestType:
		s.handleGetHighestReq(msg)
	case network.Sync_GetInstanceRange:
		s.handleGetDecidedReq(msg)
	default:
		s.logger.Error("sync req handler received un-supported type", zap.Uint64("received type", uint64(msg.Msg.Type)))
	}
}

func (s *ReqHandler) handleGetDecidedReq(msg *network.SyncChanObj) {
	if len(msg.Msg.Params) != 2 {
		panic("implement")
	}
	if msg.Msg.Params[0] > msg.Msg.Params[1] {
		panic("implement")
	}

	// enforce max page size
	startSeq := msg.Msg.Params[0]
	endSeq := msg.Msg.Params[1]
	if endSeq-startSeq > s.paginationMaxSize {
		endSeq = startSeq + s.paginationMaxSize
	}

	ret := make([]*proto.SignedMessage, 0)
	for i := startSeq; i <= endSeq; i++ {
		decidedMsg, err := s.storage.GetDecided(msg.Msg.ValidatorPk, i)
		if err != nil {
			panic("implement")
		}

		ret = append(ret, decidedMsg)
	}

	retMsg := &network.SyncMessage{
		SignedMessages: ret,
		ValidatorPk:    msg.Msg.ValidatorPk,
		Type:           network.Sync_GetInstanceRange,
	}
	if err := s.network.RespondToGetDecidedByRange(msg.Stream, retMsg); err != nil {
		s.logger.Error("failed to send get decided by range response", zap.Error(err))
	}
}

func (s *ReqHandler) handleGetHighestReq(msg *network.SyncChanObj) {
	highest, err := s.storage.GetHighestDecidedInstance(msg.Msg.ValidatorPk)
	if err != nil {
		s.logger.Error("failed to get highest decided from db", zap.Error(err))
	}
	res := &network.SyncMessage{
		SignedMessages: []*proto.SignedMessage{highest},
		Type:           network.Sync_GetHighestType,
	}

	if err := s.network.RespondToHighestDecidedInstance(msg.Stream, res); err != nil {
		s.logger.Error("failed to send highest decided response", zap.Error(err))
	}
}
