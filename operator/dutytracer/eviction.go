package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// TTL in slots for each role
const (
	ttlCommittee = 4
	ttlValidator = 4

	ttlMapping       = 4
	ttlCommitteeRoot = 4

	// depth at which we look back when evicting
	// to ensure we evict previously skipped or missed duties
	depth = 15
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
					c.logger.Info("got trace with missing validator index", fields.Validator(pk[:]), fields.Slot(slot))
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

	return
}
