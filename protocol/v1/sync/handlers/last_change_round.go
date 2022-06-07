package handlers

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// LastChangeRoundHandler handler for last-decided protocol
// TODO: add msg validation
func LastChangeRoundHandler(plogger *zap.Logger, store qbftstorage.ChangeRoundStore, reporting protocolp2p.ValidationReporting) protocolp2p.RequestHandler {
	//plogger = plogger.With(zap.String("who", "last decided handler"))
	return func(msg *message.SSVMessage) (*message.SSVMessage, error) {
		//logger := plogger.With(zap.String("msg_id_hex", fmt.Sprintf("%x", msg.ID)))
		sm := &message.SyncMessage{}
		err := sm.Decode(msg.Data)
		if err != nil {
			reporting.ReportValidation(msg, protocolp2p.ValidationRejectLow)
			sm.Status = message.StatusBadRequest
		} else if sm.Protocol != message.LastChangeRoundType {
			// not this protocol
			// TODO: remove after v0
			return nil, nil
		} else {
			res, err := store.GetLastChangeRoundMsg(msg.ID)
			if err != nil {
				logex.GetLogger().Warn("change round sync msg error", zap.Error(err))
			}
			plogger.Debug("last change round handler", zap.Any("msgs", res), zap.Error(err))
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
