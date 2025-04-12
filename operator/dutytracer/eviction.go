package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
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

func (c *Collector) evictValidatorCommitteeLinks(threshold phase0.Slot) (totalSaved int) {
	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		for slot := threshold; ; slot-- {
			committeeID, found := slotToCommittee.Get(slot)
			if !found {
				break
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

func (c *Collector) evictCommitteeTraces(threshold phase0.Slot) (totalSaved int) {
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Get(slot)
			if !found {
				break
			}

			// lock only for copying
			var local *model.CommitteeDutyTrace
			func() {
				trace.Lock()
				defer trace.Unlock()
				local = deepCopyCommitteeDutyTrace(&trace.CommitteeDutyTrace)
			}()

			if err := c.store.SaveCommitteeDuty(local); err != nil {
				c.logger.Error("save committee duty to disk", zap.Error(err))
				continue
			}

			totalSaved++

			slotToTraceMap.Delete(slot)
		}

		return true
	})

	return
}

func (c *Collector) evictValidatorTraces(threshold phase0.Slot) (totalSaved int) {
	c.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Get(slot)
			if !found {
				break
			}

			// lock only for copying
			var roles []*model.ValidatorDutyTrace
			func() {
				trace.Lock()
				defer trace.Unlock()

				for _, role := range trace.Roles {
					roles = append(roles, deepCopyValidatorDutyTrace(role))
				}
			}()

			var savedCount int

			for _, role := range roles {
				// TODO(me): confirm it makes sense
				// in case some duties do not have the validator index set
				if role.Validator == 0 {
					c.logger.Info("got trace with missing validator index", fields.Validator(pk[:]), fields.Slot(slot))
					index, found := c.validators.ValidatorIndex(pk)
					if !found {
						continue
					} else {
						role.Validator = index
					}
				}

				if err := c.store.SaveValidatorDuty(role); err != nil {
					c.logger.Error("save validator duties to disk", zap.Error(err))
					continue
				}

				savedCount++
			}

			totalSaved += savedCount

			// if all were saved, remove the slot; otherwise, keep it for the next iteration
			if savedCount == len(roles) {
				slotToTraceMap.Delete(slot)
			} else {
				c.logger.Info("not all validator duties were saved", fields.Validator(pk[:]))
			}
		}

		return true
	})

	return
}
