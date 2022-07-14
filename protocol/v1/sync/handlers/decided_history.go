package handlers

import (
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

// HistoryHandler handler for decided history protocol
// TODO: add msg validation
func HistoryHandler(plogger *zap.Logger, store qbftstorage.DecidedMsgStore, reporting protocolp2p.ValidationReporting, maxBatchSize int) protocolp2p.RequestHandler {
	plogger = plogger.With(zap.String("who", "last decided handler"))
	return func(msg *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
		logger := plogger.With(zap.String("msg_id_hex", fmt.Sprintf("%x", msg.MsgID)))
		sm := &message.SyncMessage{}
		err := sm.Decode(msg.Data)
		if err != nil {
			logger.Debug("could not decode msg data", zap.Error(err))
			reporting.ReportValidation(msg, protocolp2p.ValidationRejectLow)
			sm.Status = message.StatusBadRequest
		} else if sm.Protocol != message.DecidedHistoryType {
			// not this protocol
			// TODO: remove after v0
			return nil, nil
		} else {
			items := int(sm.Params.Height[1] - sm.Params.Height[0])
			if items > maxBatchSize {
				sm.Params.Height[1] = sm.Params.Height[0] + specqbft.Height(maxBatchSize)
			}
			results, err := store.GetDecided(msg.GetID(), sm.Params.Height[0], sm.Params.Height[1])
			sm.UpdateResults(err, results...)
		}

		data, err := sm.Encode()
		if err != nil {
			return nil, errors.Wrap(err, "could not encode result data")
		}
		msg.Data = data

		return msg, nil
	}
}
