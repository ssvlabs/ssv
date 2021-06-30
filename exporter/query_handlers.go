package exporter

import (
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/pkg/errors"
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

func getOperators(s storage.OperatorsCollection, filter api.MessageFilter) ([]storage.OperatorInformation, error) {
	var operators []storage.OperatorInformation
	if len(filter.PublicKey) > 0 {
		operator, err := s.GetOperatorInformation(filter.PublicKey)
		if err != nil {
			return nil, errors.Wrap(err, "could not read operator")
		}
		operators = append(operators, *operator)
	} else {
		var err error
		operators, err = s.ListOperators(filter.From, filter.To)
		if err != nil {
			return nil, errors.Wrap(err, "could not read operators")
		}
	}
	return operators, nil
}

func getValidators(s storage.ValidatorsCollection, filter api.MessageFilter) ([]storage.ValidatorInformation, error) {
	var validators []storage.ValidatorInformation
	if len(filter.PublicKey) > 0 {
		validator, err := s.GetValidatorInformation(filter.PublicKey)
		if err != nil {
			return nil, errors.Wrap(err, "could not read validator")
		}
		validators = append(validators, *validator)
	} else {
		var err error
		validators, err = s.ListValidators(filter.From, filter.To)
		if err != nil {
			return nil, errors.Wrap(err, "could not read validators")
		}
	}
	return validators, nil
}
