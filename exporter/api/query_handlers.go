package api

import (
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

const (
	unknownError = "unknown error"
)

type Handler struct {
	logger *zap.Logger
	store  *storage.ParticipantStores
	domain spectypes.DomainType
}

func NewHandler(
	logger *zap.Logger,
	store *storage.ParticipantStores,
	domain spectypes.DomainType,
) *Handler {
	return &Handler{
		logger: logger,
		store:  store,
		domain: domain,
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
	if len(pkRaw) != pubKeySize {
		h.logger.Warn("bad size for the provided public key", zap.Int("length", len(pkRaw)))
		res.Data = []string{"bad size for the provided public key"}
		nm.Msg = res
		return
	}

	role, err := message.BeaconRoleFromString(nm.Msg.Filter.Role)
	if err != nil {
		h.logger.Warn("failed to parse role", zap.Error(err))
		res.Data = []string{"role doesn't exist"}
		nm.Msg = res
		return
	}
	roleStorage := h.store.Get(role)
	if roleStorage == nil {
		h.logger.Warn("role storage doesn't exist", fields.BeaconRole(role))
		res.Data = []string{"internal error - role storage doesn't exist", role.String()}
		nm.Msg = res
		return
	}

	from := phase0.Slot(nm.Msg.Filter.From)
	to := phase0.Slot(nm.Msg.Filter.To)
	participantsList, err := roleStorage.GetParticipantsInRange(spectypes.ValidatorPK(pkRaw), from, to)
	if err != nil {
		h.logger.Warn("failed to get participants", zap.Error(err))
		res.Data = []string{"internal error - could not get participants messages"}
	} else {
		participations := toParticipations(role, spectypes.ValidatorPK(pkRaw), participantsList)
		data, err := ParticipantsAPIData(h.domain, participations...)
		if err != nil {
			res.Data = []string{err.Error()}
		} else {
			res.Data = data
		}
	}
	nm.Msg = res
}

func toParticipations(role spectypes.BeaconRole, pk spectypes.ValidatorPK, ee []qbftstorage.ParticipantsRangeEntry) []qbftstorage.Participation {
	out := make([]qbftstorage.Participation, 0, len(ee))
	for _, e := range ee {
		p := qbftstorage.Participation{
			ParticipantsRangeEntry: e,
			Role:                   role,
			PubKey:                 pk,
		}
		out = append(out, p)
	}

	return out
}
