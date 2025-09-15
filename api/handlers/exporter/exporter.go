package exporter

import (
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Exporter struct {
	participantStores *ibftstorage.ParticipantStores
	traceStore        dutyTraceStore
	validators        registrystorage.ValidatorStore
	logger            *zap.Logger
}

func NewExporter(logger *zap.Logger, participantStores *ibftstorage.ParticipantStores, traceStore dutyTraceStore, validators registrystorage.ValidatorStore) *Exporter {
	return &Exporter{
		participantStores: participantStores,
		traceStore:        traceStore,
		validators:        validators,
		logger:            logger,
	}
}

type dutyTraceStore interface {
	GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error)
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role ...spectypes.BeaconRole) (*exporter.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error)
	GetCommitteeID(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
}

// Common helpers shared across handlers
func toApiError(errs *multierror.Error) *api.ErrorResponse {
	if len(errs.Errors) == 1 {
		return api.Error(errs.Errors[0])
	}
	return api.Error(errs)
}

func toStrings(errs []error) []string {
	result := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			result = append(result, err.Error())
		}
	}
	return result
}

func isNotFoundError(e error) bool {
	return errors.Is(e, store.ErrNotFound) || errors.Is(e, dutytracer.ErrNotFound)
}
