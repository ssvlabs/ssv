package exporter2

import (
	"errors"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
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
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error)
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error)
}

func isNotFoundError(e error) bool {
	return errors.Is(e, store.ErrNotFound) || errors.Is(e, dutytracer.ErrNotFound)
}

func filterOutDutyNotFoundErrors(e *multierror.Error) *multierror.Error {
	if e == nil || e.ErrorOrNil() == nil {
		return nil
	}
	var filteredErrs *multierror.Error
	for _, err := range e.Errors {
		if !isNotFoundError(err) {
			filteredErrs = multierror.Append(filteredErrs, err)
		}
	}
	return filteredErrs
}

// indicesFromDecidedsQuery resolves validator indices from an DecidedsQuery
// by combining explicit indices with indices looked up from pubkeys.
func (e *Exporter) indicesFromDecidedsQuery(query *DecidedsQuery) ([]phase0.ValidatorIndex, error) {
	indices := make([]phase0.ValidatorIndex, 0, len(query.Indices)+len(query.PubKeys))
	var errs *multierror.Error

	indices = append(indices, query.Indices...)
	for _, pk := range query.PubKeys {
		i, ok := e.validators.ValidatorIndex(pk)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("validator not found for pubkey: %x", pk))
			continue
		}
		indices = append(indices, i)
	}

	slices.Sort(indices)
	indices = slices.Compact(indices)

	return indices, errs.ErrorOrNil()
}

// extractIndices resolves validator indices from an
// ValidatorTracesQuery by combining explicit indices with indices
// looked up from pubkeys.
func (e *Exporter) extractIndices(query *ValidatorTracesQuery) ([]phase0.ValidatorIndex, error) {
	indices := make([]phase0.ValidatorIndex, 0, len(query.Indices)+len(query.PubKeys))
	var errs *multierror.Error

	indices = append(indices, query.Indices...)
	for _, pk := range query.PubKeys {
		i, ok := e.validators.ValidatorIndex(pk)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("validator not found for pubkey: %x", pk))
			continue
		}
		indices = append(indices, i)
	}

	slices.Sort(indices)
	indices = slices.Compact(indices)

	return indices, errs.ErrorOrNil()
}
