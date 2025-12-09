package exporter

import (
	"errors"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	exporter2 "github.com/ssvlabs/ssv/exporter2"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"

	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
)

type Exporter struct {
	logger *zap.Logger
	svc    *exporter2.Exporter
}

func NewExporter(logger *zap.Logger, participantStores *ibftstorage.ParticipantStores, traceStore dutyTraceStore, validators registrystorage.ValidatorStore) *Exporter {
	return &Exporter{
		logger: logger,
		svc:    exporter2.NewExporter(logger, participantStores, traceStore, validators),
	}
}

type dutyTraceStore interface {
	GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error)
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role ...spectypes.BeaconRole) (*exporter.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error)
	GetCommitteeID(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error)
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error)
}

// Common helpers shared across handlers
// toApiError produces a rendered API error response, logs it with context, and
// allows callers to choose the HTTP status code. It should be the only
// function used by exporter handlers to return errors so logging remains
// consistent.
func toApiError(logger *zap.Logger, r *http.Request, endpoint string, status int, req any, err error) *api.ErrorResponse {
	if err == nil {
		err = errors.New("unknown error")
	}

	// Wrap the error into the standard response shape with the desired status.
	var apiErr *api.ErrorResponse
	if status == http.StatusBadRequest {
		apiErr = api.BadRequestError(err)
	} else {
		apiErr = &api.ErrorResponse{
			Err:     err,
			Code:    status,
			Status:  http.StatusText(status),
			Message: err.Error(),
		}
	}

	if logger != nil {
		logger.Error("exporter API request failed",
			zap.String("endpoint", endpoint),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", status),
			zap.String("error", apiErr.Message),
			zap.Any("request", req),
		)
	}

	return apiErr
}

func toStrings(err *multierror.Error) []string {
	if err.ErrorOrNil() == nil {
		return nil
	}
	errs := err.Errors
	result := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			result = append(result, err.Error())
		}
	}
	return result
}

func parsePubkeysSlice(hexSlice api.HexSlice) []spectypes.ValidatorPK {
	pubkeys := make([]spectypes.ValidatorPK, 0, len(hexSlice))
	for _, pk := range hexSlice {
		var key spectypes.ValidatorPK
		copy(key[:], pk)
		pubkeys = append(pubkeys, key)
	}
	return pubkeys
}
