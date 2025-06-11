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
)

func (c *Collector) dumpLinkToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		committeeID, found := slotToCommittee.Get(slot)
		if !found {
			return true
		}

		if err := c.store.SaveCommitteeDutyLink(slot, index, committeeID); err != nil {
			c.logger.Error("save validator to committee relations to disk", zap.Error(err))
			return true
		}

		totalSaved++

		slotToCommittee.Delete(slot)

		return true
	})

	return
}

func (c *Collector) dumpCommitteeToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		if err := c.store.SaveCommitteeDuty(trace.trace()); err != nil {
			c.logger.Error("save committee duty to disk", zap.Error(err))
			return true
		}

		totalSaved++

		slotToTraceMap.Delete(slot)

		return true
	})

	return
}

func (c *Collector) dumpValidatorToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	c.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		for _, role := range trace.roleTraces() {
			// TODO(me): confirm it makes sense
			// in case some duties do not have the validator index set
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

		return true
	})

	return
}
