package api

import (
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
)

const (
	unknownError = "unknown error"
)

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

// HandleParticipantsQuery handles TypeParticipants queries.
func HandleParticipantsQuery(logger *zap.Logger, qbftStorage *storage.QBFTStores, nm *NetworkMessage, domain spectypes.DomainType) {
	logger.Debug("handles query request",
		zap.Uint64("from", nm.Msg.Filter.From),
		zap.Uint64("to", nm.Msg.Filter.To),
		zap.String("publicKey", nm.Msg.Filter.PublicKey),
		zap.String("role", nm.Msg.Filter.Role))
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

	beaconRole, err := message.BeaconRoleFromString(nm.Msg.Filter.Role)
	if err != nil {
		logger.Warn("failed to parse role", zap.Error(err))
		res.Data = []string{"role doesn't exist"}
		nm.Msg = res
		return
	}
	runnerRole := convert.RunnerRole(beaconRole)
	roleStorage := qbftStorage.Get(runnerRole)
	if roleStorage == nil {
		logger.Warn("role storage doesn't exist", fields.ExporterRole(runnerRole))
		res.Data = []string{"internal error - role storage doesn't exist", beaconRole.String()}
		nm.Msg = res
		return
	}

	msgID := convert.NewMsgID(domain, pkRaw, runnerRole)
	from := phase0.Slot(nm.Msg.Filter.From)
	to := phase0.Slot(nm.Msg.Filter.To)
	participantsList, err := roleStorage.GetParticipantsInRange(msgID, from, to)
	if err != nil {
		logger.Warn("failed to get participants", zap.Error(err))
		res.Data = []string{"internal error - could not get participants messages"}
	} else {
		data, err := ParticipantsAPIData(participantsList...)
		if err != nil {
			res.Data = []string{err.Error()}
		} else {
			res.Data = data
		}
	}
	nm.Msg = res
}
