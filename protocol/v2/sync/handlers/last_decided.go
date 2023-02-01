package handlers

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
)

// LastDecidedHandler handler for last-decided protocol
// TODO: add msg validation and report scores
func LastDecidedHandler(plogger *zap.Logger, storeMap *storage.QBFTStores, reporting protocolp2p.ValidationReporting) protocolp2p.RequestHandler {
	plogger = plogger.With(zap.String("who", "LastDecidedHandler"))
	return func(msg *spectypes.SSVMessage) (*spectypes.SSVMessage, error) {
		logger := plogger.With(zap.String("identifier", msg.MsgID.String()))
		sm := &message.SyncMessage{}
		err := sm.Decode(msg.Data)
		if err != nil {
			logger.Debug("failed to decode message data", zap.Error(err))
			reporting.ReportValidation(msg, protocolp2p.ValidationRejectLow)
			sm.Status = message.StatusBadRequest
		} else if sm.Protocol != message.LastDecidedType {
			// not this protocol
			// TODO: remove after v0
			return nil, nil
		} else {
			msgID := msg.GetID()
			store := storeMap.Get(msgID.GetRoleType())
			if store == nil {
				return nil, errors.New(fmt.Sprintf("not storage found for type %s", msgID.GetRoleType().String()))
			}
			instance, err := store.GetHighestInstance(msgID[:])
			if err != nil {
				logger.Debug("failed to get highest instance", zap.Error(err))
			} else if instance != nil {
				sm.UpdateResults(err, instance.DecidedMessage)
			}
		}

		data, err := sm.Encode()
		if err != nil {
			return nil, errors.Wrap(err, "could not encode result data")
		}
		msg.Data = data

		return msg, nil
	}
}
