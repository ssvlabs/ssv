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
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/casts"
)

const (
	unknownError = "unknown error"
)

type Handler struct {
	logger      *zap.Logger
	qbftStorage *storage.QBFTStores
	shares      registrystorage.Shares
	domain      spectypes.DomainType
}

func NewHandler(
	logger *zap.Logger,
	qbftStorage *storage.QBFTStores,
	shares registrystorage.Shares,
	domain spectypes.DomainType,
) *Handler {
	return &Handler{
		logger:      logger,
		qbftStorage: qbftStorage,
		shares:      shares,
		domain:      domain,
	}
}

// HandleQueryRequests handles query requests
func (h *Handler) HandleQueryRequests(nm *NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = Message{Type: TypeError, Data: []string{"could not parse network message"}}
	}
	h.logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))
	switch nm.Msg.Type {
	case TypeDecided:
		h.HandleParticipantsQuery(nm)
	case TypeError:
		h.HandleErrorQuery(nm)
	default:
		h.HandleUnknownQuery(nm)
	}
}

// HandleErrorQuery handles TypeError queries.
func (h *Handler) HandleErrorQuery(nm *NetworkMessage) {
	h.logger.Warn("handles error message")
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
func (h *Handler) HandleUnknownQuery(nm *NetworkMessage) {
	h.logger.Warn("unknown message type", zap.String("messageType", string(nm.Msg.Type)))
	nm.Msg = Message{
		Type: TypeError,
		Data: []string{fmt.Sprintf("bad request - unknown message type '%s'", nm.Msg.Type)},
	}
}

// HandleParticipantsQuery handles TypeParticipants queries.
func (h *Handler) HandleParticipantsQuery(nm *NetworkMessage) {
	h.logger.Debug("handles query request",
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
		h.logger.Warn("failed to decode validator public key", zap.Error(err))
		res.Data = []string{"internal error - could not read validator key"}
		nm.Msg = res
		return
	}

	beaconRole, err := message.BeaconRoleFromString(nm.Msg.Filter.Role)
	if err != nil {
		h.logger.Warn("failed to parse role", zap.Error(err))
		res.Data = []string{"role doesn't exist"}
		nm.Msg = res
		return
	}
	runnerRole := casts.BeaconRoleToConvertRole(beaconRole)
	roleStorage := h.qbftStorage.Get(runnerRole)
	if roleStorage == nil {
		h.logger.Warn("role storage doesn't exist", fields.ExporterRole(runnerRole))
		res.Data = []string{"internal error - role storage doesn't exist", beaconRole.String()}
		nm.Msg = res
		return
	}

	share, ok := h.shares.Get(nil, pkRaw)
	if !ok {
		h.logger.Warn("share doesn't exist", fields.Validator(pkRaw))
		res.Data = []string{"internal error - role storage doesn't exist", beaconRole.String()}
		nm.Msg = res
		return
	}

	msgID := convert.NewMsgID(h.domain, pkRaw, runnerRole)
	from := phase0.Slot(nm.Msg.Filter.From)
	to := phase0.Slot(nm.Msg.Filter.To)
	operatorIDs := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, sm := range share.Committee {
		operatorIDs = append(operatorIDs, sm.Signer)
	}

	participantsList, err := roleStorage.GetParticipantsInRange(msgID, from, to, operatorIDs)
	if err != nil {
		h.logger.Warn("failed to get participants", zap.Error(err))
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
