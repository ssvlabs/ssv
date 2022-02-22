package exporter

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
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
	res := api.Message{
		Type:   nm.Msg.Type,
		Filter: nm.Msg.Filter,
	}
	if err != nil {
		logger.Error("could not get operators", zap.Error(err))
		res.Data = []string{"internal error - could not get operators"}
	} else {
		res.Data = operators
	}
	nm.Msg = res
}

func handleValidatorsQuery(logger *zap.Logger, s storage.ValidatorsCollection, nm *api.NetworkMessage) {
	logger.Debug("handles validators request",
		zap.Int64("from", nm.Msg.Filter.From),
		zap.Int64("to", nm.Msg.Filter.To),
		zap.String("pk", nm.Msg.Filter.PublicKey))
	res := api.Message{
		Type:   nm.Msg.Type,
		Filter: nm.Msg.Filter,
	}
	validators, err := getValidators(s, nm.Msg.Filter)
	if err != nil {
		logger.Warn("failed to get validators", zap.Error(err))
		res.Data = []string{"internal error - could not get validators"}
	} else {
		res.Data = validators
	}
	nm.Msg = res
}

func handleDecidedQuery(logger *zap.Logger, validatorStorage storage.ValidatorsCollection, ibftStorage collections.Iibft, nm *api.NetworkMessage) {
	logger.Debug("handles decided request",
		zap.Int64("from", nm.Msg.Filter.From),
		zap.Int64("to", nm.Msg.Filter.To),
		zap.String("pk", nm.Msg.Filter.PublicKey),
		zap.String("role", string(nm.Msg.Filter.Role)))
	res := api.Message{
		Type:   nm.Msg.Type,
		Filter: nm.Msg.Filter,
	}
	v, found, err := validatorStorage.GetValidatorInformation(nm.Msg.Filter.PublicKey)
	if err != nil {
		logger.Warn("failed to get validators", zap.Error(err))
		res.Data = []string{"internal error - could not get validator"}
	} else if !found {
		logger.Warn("validator not found")
		res.Data = []string{"internal error - could not find validator"}
	} else {
		pkRaw, err := hex.DecodeString(v.PublicKey)
		if err != nil {
			logger.Warn("failed to decode validator public key", zap.Error(err))
			res.Data = []string{"internal error - could not read validator key"}
		} else {
			identifier := format.IdentifierFormat(pkRaw, string(nm.Msg.Filter.Role))
			msgs, err := ibftStorage.GetDecidedInRange([]byte(identifier), uint64(nm.Msg.Filter.From), uint64(nm.Msg.Filter.To))
			if err != nil {
				logger.Warn("failed to get decided messages", zap.Error(err))
				res.Data = []string{"internal error - could not get decided messages"}
			} else {
				res.Data = msgs
			}
		}
	}
	nm.Msg = res
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
