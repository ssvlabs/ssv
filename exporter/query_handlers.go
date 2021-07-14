package exporter

import (
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"go.uber.org/zap"
)

const (
	unknownError = "unknown error"
)

func handleOperatorsQuery(logger *zap.Logger, storage storage.OperatorsCollection, nm *api.NetworkMessage) {
	logger.Debug("handles operators request",
		zap.Int64("from", nm.Msg.Filter.From),
		zap.Int64("to", nm.Msg.Filter.To),
		zap.String("pk", nm.Msg.Filter.PublicKey))
	operators, err := getOperators(storage, nm.Msg.Filter)
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

func handleValidatorsQuery(logger *zap.Logger, s storage.ValidatorsCollection, nm *api.NetworkMessage) {
	logger.Debug("handles validators request",
		zap.Int64("from", nm.Msg.Filter.From),
		zap.Int64("to", nm.Msg.Filter.To),
		zap.String("pk", nm.Msg.Filter.PublicKey))
	validators, err := getValidators(s, nm.Msg.Filter)
	if err != nil {
		logger.Warn("failed to get validators", zap.Error(err))
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   []string{"internal error - could not get validators"},
		}
	} else {
		nm.Msg = api.Message{
			Type:   nm.Msg.Type,
			Filter: nm.Msg.Filter,
			Data:   validators,
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

func handleErrorQuery(logger *zap.Logger, nm *api.NetworkMessage) {
	logger.Warn("handles error message")
	if _, ok := nm.Msg.Data.([]string); !ok {
		nm.Msg.Data = []string{}
	}
	errs := nm.Msg.Data.([]string)
	if nm.Err != nil {
		errs = append(errs, nm.Err.Error())
	}
	if len(errs) == 0 {
		errs = append(errs, unknownError)
	}
	nm.Msg = api.Message{
		Type: api.TypeError,
		Data: errs,
	}
}

func handleUnknownQuery(logger *zap.Logger, nm *api.NetworkMessage) {
	logger.Warn("unknown message type", zap.String("messageType", string(nm.Msg.Type)))
	nm.Msg = api.Message{
		Type: api.TypeError,
		Data: []string{fmt.Sprintf("bad request - unknown message type '%s'", nm.Msg.Type)},
	}
}
