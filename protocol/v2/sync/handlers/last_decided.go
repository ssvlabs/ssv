package handlers

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage2 "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
)

// LastDecidedHandler handler for last-decided protocol
// TODO: add msg validation and report scores
func LastDecidedHandler(plogger *zap.Logger, store map[spectypes.BeaconRole]qbftstorage2.QBFTStore, reporting protocolp2p.ValidationReporting) protocolp2p.RequestHandler {
	plogger = plogger.With(zap.String("who", "last decided handler"))
	return func(msg *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
		logger := plogger.With(zap.String("identifier", msg.MsgID.String()))
		sm := &message.SyncMessage{}
		err := sm.Decode(msg.Data)
		if err != nil {
			logger.Debug("could not decode msg data", zap.Error(err))
			reporting.ReportValidation(msg, protocolp2p.ValidationRejectLow)
			sm.Status = message.StatusBadRequest
		} else if sm.Protocol != message.LastDecidedType {
			// not this protocol
			// TODO: remove after v0
			return nil, nil
		} else {
			msgID := msg.GetID()
			res, err := store[msgID.GetRoleType()].GetLastDecided(msgID[:])
			logger.Debug("last decided results", zap.Any("res", res), zap.Error(err))
			sm.UpdateResults(err, res)
		}

		data, err := sm.Encode()
		if err != nil {
			return nil, errors.Wrap(err, "could not encode result data")
		}
		msg.Data = data

		return msg, nil
	}
}
