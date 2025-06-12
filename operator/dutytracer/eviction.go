package validator

import (
	"context"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// TTL in slots for each role
const (
	ttlCommittee = 4
	ttlValidator = 4

	ttlMapping       = 4
	ttlCommitteeRoot = 4

	// look back 5 slots
	depth = 5
)

func (c *Collector) dumpLinkToDBPeriodically(thresholdSlot phase0.Slot) (totalSaved int) {
	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		// we look back up to `depth` slots to ensure we evict previously skipped or missed duties
		for d := range depth {
			slot := thresholdSlot - phase0.Slot(d)
			committeeID, found := slotToCommittee.Get(slot)
			if !found {
				continue
			}

			if err := c.store.SaveCommitteeDutyLink(slot, index, committeeID); err != nil {
				c.logger.Error("save validator to committee relations to disk", zap.Error(err))
				return true
			}

			totalSaved++

			slotToCommittee.Delete(slot)
		}
		return true
	})

	stats, minSlot, maxSlot := 0, phase0.Slot(math.MaxInt), phase0.Slot(0)
	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		slotToCommittee.Range(func(slot phase0.Slot, committeeID spectypes.CommitteeID) bool {
			stats++
			if slot < minSlot {
				minSlot = slot
			}
			if slot > maxSlot {
				maxSlot = slot
			}
			return true
		})
		return true
	})

	c.logger.Info("dumpLinkToDBPeriodically", fields.Slot(thresholdSlot), zap.Int("count", stats), zap.Uint64("minSlot", uint64(minSlot)), zap.Uint64("maxSlot", uint64(maxSlot)))

	tracerCacheSizeHistogram.Record(context.Background(), float64(stats),
		metric.WithAttributes(
			attribute.String("type", "link"),
		),
	)

	return
}

func (c *Collector) dumpCommitteeToDBPeriodically(thresholdSlot phase0.Slot) (totalSaved int) {
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		// we look back up to `depth` slots to ensure we evict previously skipped or missed duties
		for d := range depth {
			slot := thresholdSlot - phase0.Slot(d)
			trace, found := slotToTraceMap.Get(slot)
			if !found {
				continue
			}

			if err := c.store.SaveCommitteeDuty(trace.trace()); err != nil {
				c.logger.Error("save committee duty to disk", zap.Error(err))
				return true
			}

			totalSaved++

			slotToTraceMap.Delete(slot)
		}
		return true
	})

	stats, minSlot, maxSlot := 0, phase0.Slot(math.MaxInt), phase0.Slot(0)
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, trace *committeeDutyTrace) bool {
			if slot < minSlot {
				minSlot = slot
			}
			if slot > phase0.Slot(maxSlot) {
				maxSlot = slot
			}
			stats++
			return true
		})
		return true
	})

	c.logger.Info("dumpCommitteeToDBPeriodically", fields.Slot(thresholdSlot), zap.Int("count", stats), zap.Uint64("minSlot", uint64(minSlot)), zap.Uint64("maxSlot", uint64(maxSlot)))

	tracerCacheSizeHistogram.Record(context.Background(), float64(stats),
		metric.WithAttributes(
			attribute.String("type", "committee"),
		),
	)

	return
}

func (c *Collector) dumpValidatorToDBPeriodically(thresholdSlot phase0.Slot) (totalSaved int) {
	c.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		// we look back up to `depth` slots to ensure we evict previously skipped or missed duties
		for d := range depth {
			slot := thresholdSlot - phase0.Slot(d)
			trace, found := slotToTraceMap.Get(slot)
			if !found {
				continue
			}

			for _, role := range trace.roleTraces() {
				if role.Validator == 0 {
					index, found := c.validators.ValidatorIndex(pk)
					if !found {
						continue
					}

					role.Validator = index
				}

				if err := c.store.SaveValidatorDuty(role); err != nil {
					c.logger.Error("save validator duties to disk", zap.Error(err))
					return true
				}

				totalSaved++
			}

			slotToTraceMap.Delete(slot)
		}
		return true
	})

	stats, minSlot, maxSlot := 0, phase0.Slot(math.MaxInt), phase0.Slot(0)
	c.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		slotToTraceMap.Range(func(slot phase0.Slot, trace *validatorDutyTrace) bool {
			stats++
			if slot < minSlot {
				minSlot = slot
			}
			if slot > maxSlot {
				maxSlot = slot
			}
			return true
		})
		return true
	})

	c.logger.Info("dumpValidatorToDBPeriodically", fields.Slot(thresholdSlot), zap.Int("count", stats), zap.Uint64("minSlot", uint64(minSlot)), zap.Uint64("maxSlot", uint64(maxSlot)))

	tracerCacheSizeHistogram.Record(context.Background(), float64(stats),
		metric.WithAttributes(
			attribute.String("type", "validator"),
		),
	)

	return
}
