package handlers

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
)

// HistoryHandler handler for decided history protocol
// TODO: add msg validation and report scores
func HistoryHandler(logger *zap.Logger, storeMap *storage.QBFTStores, reporting protocolp2p.ValidationReporting, maxBatchSize int) protocolp2p.RequestHandler {
	logger = logger.With(zap.String("who", "HistoryHandler"))
	return func(msg *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
		logger := logger.With(zap.String("msg_id", fmt.Sprintf("%x", msg.MsgID)))
		sm := &message.SyncMessage{}
		err := sm.Decode(msg.Data)
		if err != nil {
			logger.Debug("failed to decode message data", zap.Error(err))
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
			msgID := msg.GetID()
			store := storeMap.Get(msgID.GetRoleType())
			if store == nil {
				return nil, errors.New(fmt.Sprintf("not storage found for type %s", msgID.GetRoleType().String()))
			}
			instances, err := store.GetInstancesInRange(msgID[:], sm.Params.Height[0], sm.Params.Height[1])
			results := make([]*specqbft.SignedMessage, 0, len(instances))
			for _, instance := range instances {
				results = append(results, instance.DecidedMessage)
			}
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
