package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

func (c *Collector) evictValidatorCommitteeLinks(slot phase0.Slot) {
	threshold := slot - getTTL(ttlMapping)

	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]) bool {
		for slot := threshold; ; slot-- {
			committeeID, found := slotToCommittee.Load(slot)
			if !found {
				break
			}

			if err := c.store.SaveCommitteeDutyLink(slot, index, committeeID); err != nil {
				c.logger.Error("save validator to committee relations to disk", zap.Error(err))
				return true
			}

			slotToCommittee.Delete(slot)
		}

		return true
	})
}

func (c *Collector) evictCommitteeTraces(currentSlot phase0.Slot) {
	threshold := currentSlot - getTTL(ttlCommittee)

	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *TypedSyncMap[phase0.Slot, *committeeDutyTrace]) bool {
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Load(slot)
			if !found {
				break
			}

			trace.Lock()
			defer trace.Unlock()

			if err := c.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace); err != nil {
				c.logger.Error("save committee duty to disk", zap.Error(err))
				continue
			}

			slotToTraceMap.Delete(slot)
		}

		return true
	})
}

func (c *Collector) evictValidatorTraces(slot phase0.Slot) {
	c.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *TypedSyncMap[phase0.Slot, *validatorDutyTrace]) bool {
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
					index, found := c.validators.ValidatorIndex(pk)
					if !found {
						c.logger.Error("no validator index", fields.Validator(pk[:]))
					} else {
						trace.Validator = index
					}
				}

				if err := c.store.SaveValidatorDuty(trace); err != nil {
					c.logger.Error("save validator duties to disk", zap.Error(err))
					continue
				}

				savedCount++
			}

			// if all were saved, remove the slot; otherwise, keep it for the next iteration
			if savedCount == len(trace.Roles) {
				slotToTraceMap.Delete(slot)
			}
		}

		return true
	})
}
