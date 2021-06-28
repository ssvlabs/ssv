package exporter

import (
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
)

func handleOperatorsQuery(logger *zap.Logger, storage Storage, nm *api.NetworkMessage) {
	operators, err := storage.ListOperators(nm.Msg.Filter.From, nm.Msg.Filter.To)
	if err != nil {
		logger.Error("could not get operators", zap.Error(err))
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   []string{"internal error - could not get operators"},
		}
	} else {
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   operators,
		}
	}
}

func handleValidatorsQuery(logger *zap.Logger, validatorStorage validatorstorage.ICollection, nm *api.NetworkMessage) {
	validators, err := validatorStorage.GetAllValidatorsShare()
	if err != nil {
		logger.Warn("could not get validators", zap.Error(err))
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   []string{"internal error - could not get validators"},
		}
	} else {
		var validatorMsgs []api.ValidatorInformation
		for _, v := range validators {
			validatorMsg := toValidatorMessage(v)
			validatorMsgs = append(validatorMsgs, *validatorMsg)
		}
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   validatorMsgs,
		}
	}
}

func handleDutiesQuery(logger *zap.Logger, nm *api.NetworkMessage) {
	logger.Warn("not implemented yet", zap.String("messageType", string(nm.Msg.Type)))
	nm.Msg = api.Message{
		Type: api.TypeError,
		Data: []string{"bad request - not implemented yet"},
	}
}

func handleUnknownQuery(logger *zap.Logger, nm *api.NetworkMessage) {
	logger.Warn("unknown message type", zap.String("messageType", string(nm.Msg.Type)))
	nm.Msg = api.Message{
		Type: api.TypeError,
		Data: []string{fmt.Sprintf("bad request - unknown message type '%s'", nm.Msg.Type)},
	}
}
