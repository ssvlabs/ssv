package api

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	exportercore "github.com/ssvlabs/ssv/exporter"
	dutytracer "github.com/ssvlabs/ssv/exporter/dutytracer"
	exporterstore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
)

type validatorIndexReader interface {
	ValidatorIndex(spectypes.ValidatorPK) (phase0.ValidatorIndex, bool)
}

// HandleQueryRequests dispatches websocket query messages to either the legacy
// participant-store path or the archive exporter-core compatibility path.
func (h *Handler) HandleQueryRequests(store *storage.ParticipantStores, exporterRead *exportercore.Exporter, validators validatorIndexReader, domain spectypes.DomainType, nm *NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = Message{
			Type: TypeError,
			Data: []string{fmt.Sprintf("could not parse network message: %v", nm.Err)},
		}
	}
	h.logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))

	switch nm.Msg.Type {
	case TypeDecided:
		// In exporter archive mode we serve decided queries via exporter core.
		// Fall back to legacy qbft storage when exporter isn't wired.
		if exporterRead != nil {
			h.handleDecidedViaExporter(exporterRead, validators, domain, nm)
			break
		}
		h.HandleParticipantsQuery(store, nm, domain)
	case TypeError:
		h.HandleErrorQuery(nm)
	default:
		h.HandleUnknownQuery(nm)
	}
}

func (h *Handler) handleDecidedViaExporter(exporterRead *exportercore.Exporter, validators validatorIndexReader, domain spectypes.DomainType, nm *NetworkMessage) {
	res := Message{Type: nm.Msg.Type, Filter: nm.Msg.Filter}

	pkBytes, err := hex.DecodeString(nm.Msg.Filter.PublicKey)
	if err != nil {
		h.logger.Warn("failed to decode validator public key", zap.Error(err))
		res.Type = TypeError
		res.Data = []string{fmt.Sprintf("invalid publicKey %q: %v", nm.Msg.Filter.PublicKey, err)}
		nm.Msg = res
		return
	}

	var pk spectypes.ValidatorPK
	copy(pk[:], pkBytes)

	idx, ok := validators.ValidatorIndex(pk)
	if !ok {
		h.logger.Warn("validator not found for public key", zap.String("validator_pubkey", hex.EncodeToString(pk[:])))
		res.Type = TypeError
		res.Data = []string{fmt.Sprintf("validator not found for public key %s", nm.Msg.Filter.PublicKey)}
		nm.Msg = res
		return
	}

	role, err := message.BeaconRoleFromString(nm.Msg.Filter.Role)
	if err != nil {
		h.logger.Warn("failed to parse role", zap.Error(err))
		res.Type = TypeError
		res.Data = []string{fmt.Sprintf("role doesn't exist: %q", nm.Msg.Filter.Role)}
		nm.Msg = res
		return
	}

	coreQuery := &exportercore.DecidedsQuery{
		From:    nm.Msg.Filter.From,
		To:      nm.Msg.Filter.To,
		Roles:   []spectypes.BeaconRole{role},
		Indices: []phase0.ValidatorIndex{idx},
	}

	result, errs := exporterRead.TraceDecidedsCore(coreQuery)
	participations := wsParticipationsFromCore(result)

	var unexpectedErr error
	filtered := filterOutDutyNotFoundErrors(errs)
	if filtered.ErrorOrNil() != nil {
		for _, e := range filtered.Errors {
			// Preserve legacy WS leniency: treat validation errors as "no messages".
			if isExporterValidationError(e) {
				continue
			}
			unexpectedErr = e
		}
	}

	if len(participations) == 0 {
		if unexpectedErr != nil {
			h.logger.Warn("failed to build participants api data due to exporter errors", zap.Error(unexpectedErr), fields.ValidatorIndex(idx))
			res.Type = TypeError
			res.Data = []string{fmt.Sprintf("internal error - could not build response: %v", unexpectedErr)}
		} else {
			// Mirror legacy exporter behavior: empty range returns "no messages" as a decided response.
			res.Data = []string{"no messages"}
		}
		nm.Msg = res
		return
	}

	data, err := ParticipantsAPIData(domain, participations...)
	if err != nil {
		h.logger.Warn("failed to build participants api data", zap.Error(err))
		res.Type = TypeError
		res.Data = []string{fmt.Sprintf("internal error - could not build response: %v", err)}
		nm.Msg = res
		return
	}

	res.Data = data
	nm.Msg = res
}

func wsParticipationsFromCore(result *exportercore.TraceDecidedsResult) []storage.Participation {
	out := make([]storage.Participation, 0)
	if result == nil {
		return out
	}
	for _, p := range result.Participants {
		out = append(out, storage.Participation{
			ParticipantsRangeEntry: storage.ParticipantsRangeEntry{
				Slot:    p.Slot,
				PubKey:  p.PubKey,
				Signers: p.Signers,
			},
			Role:   p.Role,
			PubKey: p.PubKey,
		})
	}
	return out
}

func isExporterValidationError(err error) bool {
	var ve *exportercore.ValidationError
	return errors.As(err, &ve)
}

// isNotFoundError returns true if the error represents an expected "no duty"
// condition, either from the duty tracer or the underlying exporter store.
func isNotFoundError(err error) bool {
	return errors.Is(err, dutytracer.ErrNotFound) || errors.Is(err, exporterstore.ErrNotFound)
}

func filterOutDutyNotFoundErrors(e *multierror.Error) *multierror.Error {
	if e == nil || e.ErrorOrNil() == nil {
		return nil
	}
	var filtered *multierror.Error
	for _, err := range e.Errors {
		if err != nil && !isNotFoundError(err) {
			filtered = multierror.Append(filtered, err)
		}
	}
	return filtered
}
