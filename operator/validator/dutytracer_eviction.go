package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"
)

// TTL in slots for each role
const (
	ttlCommittee                 = 2
	ttlProposer                  = 2
	ttlSyncCommitteeContribution = 2
	ttlValidatorRegistration     = 2
	ttlVoluntaryExit             = 2
	ttlMapping                   = 2
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

func (tracer *InMemTracer) evictValidatorCommitteeMapping(slot phase0.Slot) {
	threshold := slot - getTTL(ttlMapping)

	tracer.validatorIndexToCommitteeMapping.Range(func(index phase0.ValidatorIndex, slotToCommittee *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]) bool {
		for slot := threshold; ; slot-- {
			committeeID, found := slotToCommittee.Load(slot)
			if !found {
				break
			}
			if err := tracer.store.SaveCommitteeDutyLink(slot, index, committeeID); err != nil {
				tracer.logger.Error("save validator to committee relations to disk", zap.Error(err))
				return true
			}

			slotToCommittee.Delete(slot)
		}

		return true
	})
}

func (tracer *InMemTracer) evictCommitteeTraces(slot phase0.Slot) {
	stats := make(map[phase0.Slot]uint) // TODO(me): replace by proper observability
	tracer.logger.Info("evicting committee traces", zap.Uint64("current slot", uint64(slot)))

	threshold := slot - getTTL(ttlCommittee)

	tracer.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *TypedSyncMap[phase0.Slot, *committeeDutyTrace]) bool {
		for slot := threshold; ; slot-- {
			trace, found := slotToTraceMap.Load(slot)
			if !found {
				break
			}

			trace.Lock()
			defer trace.Unlock()

			if err := tracer.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace); err != nil {
				tracer.logger.Error("save committee duty to disk", zap.Error(err))
				continue
			}

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

				if err := tracer.store.SaveValidatorDuty(trace); err != nil {
					tracer.logger.Error("save validator duties to disk", zap.Error(err))
					continue
				}

				savedCount++
			}

			if savedCount == len(trace.Roles) {
				slotToTraceMap.Delete(slot)
			}
		}

		return true
	})
}
