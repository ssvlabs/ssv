package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func (c *Collector) dumpLinkToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var links = make(map[phase0.ValidatorIndex]spectypes.CommitteeID)
	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		committeeID, found := slotToCommittee.Get(slot)
		if !found {
			return true
		}

		links[index] = committeeID

		return true
	})

	if err := c.store.SaveCommitteeDutyLinks(slot, links); err != nil {
		c.logger.Error("save validator to committee relations to disk", zap.Error(err))
		return
	}

	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		slotToCommittee.Delete(slot)
		return true
	})

	totalSaved = len(links)

	return
}

func (c *Collector) dumpCommitteeToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var duties []*exporter.CommitteeDutyTrace
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		data := trace.trace()
		duties = append(duties, data)
		return true
	})

	if err := c.store.SaveCommitteeDuties(slot, duties); err != nil {
		c.logger.Error("save committee duties to disk", zap.Error(err))
		return
	}

	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		slotToTraceMap.Delete(slot)
		return true
	})

	totalSaved = len(duties)

	return
}

func (c *Collector) dumpValidatorToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var duties []*exporter.ValidatorDutyTrace
	c.validatorTraces.Range(func(idx phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		for _, role := range trace.roleTraces() {
			if role.Validator == 0 {
				c.logger.Info("got trace with missing validator index", fields.ValidatorIndex(idx), fields.Slot(slot))
				role.Validator = idx
			}

			duties = append(duties, role)
		}

		return true
	})

	if err := c.store.SaveValidatorDuties(duties); err != nil {
		c.logger.Error("couldn't save validator duties to disk", zap.Error(err))
		return 0
	}

	c.validatorTraces.Range(func(_ phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		slotToTraceMap.Delete(slot)
		return true
	})

	return len(duties)
}
