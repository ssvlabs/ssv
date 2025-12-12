package validator

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/exporter/store"
)

type DutyTraceStoreMetrics struct {
	Store *store.DutyTraceStore
}

func record(col, op string, start time.Time) {
	duration := time.Since(start)
	tracerDBDurationHistogram.Record(
		context.Background(),
		duration.Seconds(),
		metric.WithAttributes(
			semconv.DBCollectionName(col),
			semconv.DBOperationName(op),
		),
	)
}

func (d *DutyTraceStoreMetrics) SaveCommitteeDutyLinks(slot phase0.Slot, linkMap map[phase0.ValidatorIndex]spectypes.CommitteeID) error {
	start := time.Now()
	defer func() {
		record("link", "save_all", start)
	}()
	return d.Store.SaveCommitteeDutyLinks(slot, linkMap)
}

func (d *DutyTraceStoreMetrics) SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error {
	start := time.Now()
	defer func() {
		record("link", "save", start)
	}()
	return d.Store.SaveCommitteeDutyLink(slot, index, id)
}

func (d *DutyTraceStoreMetrics) GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	start := time.Now()
	defer func() {
		record("link", "get", start)
	}()
	return d.Store.GetCommitteeDutyLink(slot, index)
}

func (d *DutyTraceStoreMetrics) GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error) {
	start := time.Now()
	defer func() {
		record("link", "get_all", start)
	}()
	return d.Store.GetCommitteeDutyLinks(slot)
}

func (d *DutyTraceStoreMetrics) SaveCommitteeDuties(slot phase0.Slot, duties []*exporter.CommitteeDutyTrace) error {
	start := time.Now()
	defer func() {
		record("committee", "save_all", start)
	}()
	return d.Store.SaveCommitteeDuties(slot, duties)
}

func (d *DutyTraceStoreMetrics) SaveCommitteeDuty(duty *exporter.CommitteeDutyTrace) error {
	start := time.Now()
	defer func() {
		record("committee", "save", start)
	}()
	return d.Store.SaveCommitteeDuty(duty)
}

func (d *DutyTraceStoreMetrics) GetCommitteeDuty(slot phase0.Slot, id spectypes.CommitteeID) (duty *exporter.CommitteeDutyTrace, err error) {
	start := time.Now()
	defer func() {
		record("committee", "get", start)
	}()
	return d.Store.GetCommitteeDuty(slot, id)
}

func (d *DutyTraceStoreMetrics) GetCommitteeDuties(slot phase0.Slot) ([]*exporter.CommitteeDutyTrace, error) {
	start := time.Now()
	defer func() {
		record("committee", "get_all", start)
	}()
	return d.Store.GetCommitteeDuties(slot)
}

func (d *DutyTraceStoreMetrics) SaveScheduled(slot phase0.Slot, schedule map[phase0.ValidatorIndex]rolemask.Mask) error {
	start := time.Now()
	defer func() {
		record("schedule", "save", start)
	}()
	return d.Store.SaveScheduled(slot, schedule)
}

func (d *DutyTraceStoreMetrics) GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error) {
	start := time.Now()
	defer func() {
		record("schedule", "get", start)
	}()
	return d.Store.GetScheduled(slot)
}

func (d *DutyTraceStoreMetrics) SaveValidatorDuties(duties []*exporter.ValidatorDutyTrace) error {
	start := time.Now()
	defer func() {
		record("validator", "save_all", start)
	}()
	return d.Store.SaveValidatorDuties(duties)
}

func (d *DutyTraceStoreMetrics) SaveValidatorDuty(duty *exporter.ValidatorDutyTrace) error {
	start := time.Now()
	defer func() {
		record("validator", "save", start)
	}()
	return d.Store.SaveValidatorDuty(duty)
}

func (d *DutyTraceStoreMetrics) GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error) {
	start := time.Now()
	defer func() {
		record("validator", "get", start)
	}()
	return d.Store.GetValidatorDuty(slot, role, index)
}

func (d *DutyTraceStoreMetrics) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error) {
	start := time.Now()
	defer func() {
		record("validator", "get_all", start)
	}()
	return d.Store.GetValidatorDuties(role, slot)
}
