package handlers

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// LastDecidedHandler handler for last-decided protocol
// TODO: add msg validation
func LastDecidedHandler(plogger *zap.Logger, store qbftstorage.DecidedMsgStore, reporting protocolp2p.ValidationReporting) protocolp2p.RequestHandler {
	plogger = plogger.With(zap.String("who", "last decided handler"))
	return func(msg *message.SSVMessage) (*message.SSVMessage, error) {
		logger := plogger.With(zap.String("msg_id_hex", fmt.Sprintf("%x", msg.ID)))
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
			res, err := store.GetLastDecided(msg.ID)
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
