package incoming

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

// ReqHandler is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type ReqHandler struct {
	// paginationMaxSize is the max number of returned elements in a single response
	paginationMaxSize uint64
	identifier        []byte
	network           network.Network
	storage           collections.Iibft
	logger            *zap.Logger
}

// New returns a new instance of ReqHandler
func New(logger *zap.Logger, identifier []byte, network network.Network, storage collections.Iibft) *ReqHandler {
	return &ReqHandler{
		paginationMaxSize: network.MaxBatch(),
		logger:            logger,
		identifier:        identifier,
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
