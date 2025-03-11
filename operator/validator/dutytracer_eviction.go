package validator

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
)

// TTL in slots for each role
const (
	ttlCommittee                 = 4
	ttlProposer                  = 4
	ttlSyncCommitteeContribution = 4
	ttlValidatorRegistration     = 4
	ttlVoluntaryExit             = 4

	ttlMapping = 4
	ttlRoot    = 4
)

func getTTL(role spectypes.BeaconRole) phase0.Slot {
	switch role {
	case spectypes.BNRoleProposer:
		return ttlProposer
	case spectypes.BNRoleAggregator:
		return ttlSyncCommitteeContribution
	case spectypes.BNRoleValidatorRegistration:
		return ttlValidatorRegistration
	case spectypes.BNRoleVoluntaryExit:
		return ttlVoluntaryExit
	}
	return ttlProposer
}

func (tracer *InMemTracer) evictValidatorCommitteeLinks(slot phase0.Slot) {
	threshold := slot - getTTL(ttlMapping)

	tracer.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]) bool {
		for slot := threshold; ; slot-- {
			committeeID, found := slotToCommittee.Load(slot)
			if !found {
				break
			}

			start := time.Now()

			if err := tracer.store.SaveCommitteeDutyLink(slot, index, committeeID); err != nil {
				tracer.logger.Error("save validator to committee relations to disk", zap.Error(err))
				return true
			}

			duration := time.Since(start)
			tracerDBDurationHistogram.Record(
				context.Background(),
				duration.Seconds(),
				metric.WithAttributes(
					semconv.DBCollectionName("link"),
					semconv.DBOperationName("save"),
				),
			)

			slotToCommittee.Delete(slot)
		}

		return true
	})
}

func (tracer *InMemTracer) evictCommitteeTraces(currentSlot phase0.Slot) {
	stats := make(map[phase0.Slot]uint) // TODO(me): replace by proper observability

	threshold := currentSlot - getTTL(ttlCommittee)

	tracer.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *TypedSyncMap[phase0.Slot, *committeeDutyTrace]) bool {
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Load(slot)
			if !found {
				break
			}

			trace.Lock()
			defer trace.Unlock()

			start := time.Now()

			if err := tracer.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace); err != nil {
				tracer.logger.Error("save committee duty to disk", zap.Error(err))
				continue
			}

			duration := time.Since(start)
			tracerDBDurationHistogram.Record(
				context.Background(),
				duration.Seconds(),
				metric.WithAttributes(
					semconv.DBCollectionName("committee"),
					semconv.DBOperationName("save"),
				),
			)

			stats[slot]++
			slotToTraceMap.Delete(slot)
		}

		return true
	})

	for slot, count := range stats {
		tracer.logger.Info("evicted committee trace", zap.Uint64("slot", uint64(slot)), zap.Uint("count", count))
	}
}

func (tracer *InMemTracer) evictValidatorTraces(slot phase0.Slot) {
	stats := make(map[phase0.Slot]uint) // TODO(me): replace by proper observability
	tracer.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *TypedSyncMap[phase0.Slot, *validatorDutyTrace]) bool {
		threshold := slot - getTTL(ttlCommittee)
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Load(slot)
			if !found {
				break
			}

			trace.Lock()
			defer trace.Unlock()

			var savedCount int

			for _, trace := range trace.Roles {
				// TODO: confirm it makes sense
				// in case some duties do not have the validator index set
				if trace.Validator == 0 {
					index, found := tracer.validators.ValidatorIndex(pk)
					if !found {
						tracer.logger.Error("no validator index", fields.Validator(pk[:]))
						continue
					}
					trace.Validator = index
				}

				start := time.Now()

				if err := tracer.store.SaveValidatorDuty(trace); err != nil {
					tracer.logger.Error("save validator duties to disk", zap.Error(err))
					continue
				}

				duration := time.Since(start)
				tracerDBDurationHistogram.Record(
					context.Background(),
					duration.Seconds(),
					metric.WithAttributes(
						semconv.DBCollectionName("validator"),
						semconv.DBOperationName("save"),
					),
				)

				stats[slot]++

				savedCount++
			}

			// if all were saved, remove the slot; otherwise, keep it for the next iteration
			if savedCount == len(trace.Roles) {
				slotToTraceMap.Delete(slot)
			}
		}

		return true
	})

	for slot, count := range stats {
		tracer.logger.Info("evicted validator trace", zap.Uint64("slot", uint64(slot)), zap.Uint("count", count))
	}
}
