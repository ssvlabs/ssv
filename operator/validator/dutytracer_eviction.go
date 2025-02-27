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
	mappings := make(map[phase0.ValidatorIndex]spectypes.CommitteeID)
	tracer.valToComMapping.Range(func(index phase0.ValidatorIndex, slotToCommittee *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]) bool {
		// collect all slots to delete
		var slotsToDelete []phase0.Slot

		threshold := slot - getTTL(ttlMapping)

		slotToCommittee.Range(func(slot phase0.Slot, committeeID spectypes.CommitteeID) bool {
			if slot > threshold {
				return true
			}
			mappings[index] = committeeID
			slotsToDelete = append(slotsToDelete, slot)
			return true
		})

		for _, slot := range slotsToDelete {
			slotToCommittee.Delete(slot)
		}

		return true
	})

	if err := tracer.store.SaveCommitteeDutyLinks(slot, mappings); err != nil {
		tracer.logger.Error("save validator to committee relations to disk", zap.Error(err))
	}
}

func (tracer *InMemTracer) evictCommitteeTraces(currentSlot phase0.Slot) {
	stats := make(map[phase0.Slot]uint) // TODO(me): replace by proper observability
	tracer.logger.Info("evicting committee traces", zap.Uint64("current slot", uint64(currentSlot)))

	threshold := currentSlot - getTTL(ttlCommittee)

	tracer.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *TypedSyncMap[phase0.Slot, *committeeDutyTrace]) bool {
		// collect all slots to delete
		var slotsToDelete []phase0.Slot

		slotToTraceMap.Range(func(slot phase0.Slot, trace *committeeDutyTrace) bool {
			if slot > threshold {
				return true
			}

			trace.Lock()
			defer trace.Unlock()

			if err := tracer.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace); err != nil {
				tracer.logger.Error("save committee duty to disk", zap.Error(err))
				return true // continue?
			}

			slotsToDelete = append(slotsToDelete, slot)

			return true
		})

		// once we moved all slot traces to disk, delete them from memory
		for _, slot := range slotsToDelete {
			stats[slot]++
			slotToTraceMap.Delete(slot)
		}

		return true
	})

	for slot, count := range stats {
		tracer.logger.Info("evicted committee trace", zap.Uint64("slot", uint64(slot)), zap.Uint("count", count))
	}
}

func (tracer *InMemTracer) evictValidatorTraces(currentSlot phase0.Slot) {
	tracer.validatorTraces.Range(func(pk spectypes.ValidatorPK, slotToTraceMap *TypedSyncMap[phase0.Slot, *validatorDutyTrace]) bool {
		// collect all slots to delete
		var slotsToDelete []phase0.Slot

		slotToTraceMap.Range(func(slot phase0.Slot, trace *validatorDutyTrace) bool {
			trace.Lock()
			defer trace.Unlock()

			for _, duty := range trace.Roles {
				threshold := currentSlot - getTTL(duty.Role)
				if slot > threshold {
					return true
				}

				// move to disk
				trace.Lock()
				defer trace.Unlock()

				var allSaved bool

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

					allSaved = true
				}

				if allSaved {
					slotsToDelete = append(slotsToDelete, slot)
				}
			}

			return true
		})

		// once we moved all slot traces to disk, delete them from memory
		for _, slot := range slotsToDelete {
			slotToTraceMap.Delete(slot)
		}

		return true
	})
}
