package validator

import (
	ibftController "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

// Reader handle messages for non committee validators
type Reader struct {
	logger      *zap.Logger
	ibftStorage collections.Iibft
}

// NewReader return new reader struct
func NewReader(logger *zap.Logger, db basedb.IDb) ibftController.MediatorReader {
	storage := collections.NewIbft(db, logger, beacon.RoleTypeAttester.String()) // TODO role needs to be passed
	return Reader{
		logger:      logger,
		ibftStorage: &storage,
	}
}

// GetMsgResolver return message resolver
func (r Reader) GetMsgResolver(networkMsg network.NetworkMsg) func(msg *proto.SignedMessage) {
	return func(msg *proto.SignedMessage) {
		if networkMsg == network.NetworkMsg_DecidedType {
			r.logger.Debug("received decided message for non committee validator", getFields(msg)...)
			//	save to storage
			if err := r.ibftStorage.SaveDecided(msg); err != nil { // TODO this func save decided for each seqNum. might need on this version to save only latest?
				r.logger.Error("failed to save decided msg", zap.Error(err))
			}
		}
		// pass
	}
}
