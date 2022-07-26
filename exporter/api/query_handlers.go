package api

import (
	"encoding/hex"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

const (
	unknownError = "unknown error"
)

// HandleDecidedQuery handles TypeDecided queries.
func HandleDecidedQuery(logger *zap.Logger, qbftStorage qbftstorage.QBFTStore, nm *NetworkMessage) {
	logger.Debug("handles decided request",
		zap.Uint64("from", nm.Msg.Filter.From),
		zap.Uint64("to", nm.Msg.Filter.To),
		zap.String("pk", nm.Msg.Filter.PublicKey),
		zap.String("role", string(nm.Msg.Filter.Role)))
	res := Message{
		Type:   nm.Msg.Type,
		Filter: nm.Msg.Filter,
	}

	pkRaw, err := hex.DecodeString(nm.Msg.Filter.PublicKey)
	if err != nil {
		logger.Warn("failed to decode validator public key", zap.Error(err))
		res.Data = []string{"internal error - could not read validator key"}
		nm.Msg = res
		return
	}

	identifier := spectypes.NewMsgID(pkRaw, message.RoleTypeFromString(string(nm.Msg.Filter.Role)))
	from := specqbft.Height(nm.Msg.Filter.From)
	to := specqbft.Height(nm.Msg.Filter.To)
	msgs, err := qbftStorage.GetDecided(identifier[:], from, to)
	if err != nil {
		logger.Warn("failed to get decided messages", zap.Error(err))
		res.Data = []string{"internal error - could not get decided messages"}
	} else {
		data, err := DecidedAPIData(msgs...)
		if err != nil {
			res.Data = []string{}
		} else {
			res.Data = data
		}
	}

	nm.Msg = res
}

// HandleErrorQuery handles TypeError queries.
func HandleErrorQuery(logger *zap.Logger, nm *NetworkMessage) {
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
	nm.Msg = Message{
		Type: TypeError,
		Data: errs,
	}
}

// HandleUnknownQuery handles unknown queries.
func HandleUnknownQuery(logger *zap.Logger, nm *NetworkMessage) {
	logger.Warn("unknown message type", zap.String("messageType", string(nm.Msg.Type)))
	nm.Msg = Message{
		Type: TypeError,
		Data: []string{fmt.Sprintf("bad request - unknown message type '%s'", nm.Msg.Type)},
	}
}
