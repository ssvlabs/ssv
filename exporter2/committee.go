package exporter2

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// CommitteeTracesCore contains the core logic for CommitteeTraces without any HTTP concerns.
func (e *Exporter) CommitteeTracesCore(request *CommitteeTracesQuery) (*CommitteeTracesResult, *multierror.Error) {
	if err := validateCommitteeRequest(request); err != nil {
		return nil, multierror.Append(nil, &ValidationError{Err: err})
	}

	var all []*exporter.CommitteeDutyTrace
	var errs *multierror.Error
	cids := request.CommitteeIDs
	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		duties, err := e.getCommitteeDutiesForSlot(slot, cids)
		all = append(all, duties...)
		errs = multierror.Append(errs, err)
	}

	// by design, not found duties are expected and not considered as API errors
	errs = filterOutDutyNotFoundErrors(errs)

	// Attach read-only schedule unioned per committee for the requested slot range.
	schedule := e.buildCommitteeSchedule(request)

	return &CommitteeTracesResult{Traces: all, Schedule: schedule}, errs
}

func validateCommitteeRequest(request *CommitteeTracesQuery) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}
	return nil
}

func (e *Exporter) getCommitteeDutiesForSlot(slot phase0.Slot, committeeIDs []spectypes.CommitteeID) ([]*exporter.CommitteeDutyTrace, error) {
	if len(committeeIDs) == 0 {
		duties, err := e.traceStore.GetCommitteeDuties(slot)
		return duties, err
	}

	duties := make([]*exporter.CommitteeDutyTrace, 0, len(committeeIDs))

	var errs *multierror.Error
	for _, cmtID := range committeeIDs {
		duty, err := e.traceStore.GetCommitteeDuty(slot, cmtID)
		if err != nil {
			e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.CommitteeID(cmtID))
			errs = multierror.Append(errs, err)
			continue
		}
		duties = append(duties, duty)
	}
	return duties, errs.ErrorOrNil()
}

// buildCommitteeSchedule constructs per-committee schedules by grouping scheduled indices
// via stored validator→committee links at each slot in-range.
func (e *Exporter) buildCommitteeSchedule(req *CommitteeTracesQuery) []CommitteeScheduleEntry {
	out := make([]CommitteeScheduleEntry, 0)
	// Optional filter for committees.
	var filter map[spectypes.CommitteeID]struct{}
	if len(req.CommitteeIDs) > 0 {
		filter = make(map[spectypes.CommitteeID]struct{}, len(req.CommitteeIDs))
		for _, id := range req.CommitteeIDs {
			filter[id] = struct{}{}
		}
	}

	for s := req.From; s <= req.To; s++ {
		slot := phase0.Slot(s)
		sched, err := e.traceStore.GetScheduled(slot)
		if err != nil {
			e.logger.Warn("get scheduled failed", zap.Error(err), fields.Slot(slot))
			continue
		}
		if len(sched) == 0 {
			continue
		}
		links, err := e.traceStore.GetCommitteeDutyLinks(slot)
		if err != nil {
			e.logger.Warn("get committee links failed", zap.Error(err), fields.Slot(slot))
			continue
		}
		if len(links) == 0 {
			continue
		}
		// committeeID -> role -> indices
		grouped := make(map[spectypes.CommitteeID]map[spectypes.BeaconRole][]phase0.ValidatorIndex)
		for _, l := range links {
			mask, ok := sched[l.ValidatorIndex]
			if !ok {
				continue
			}
			cid := l.CommitteeID
			if filter != nil {
				if _, ok := filter[cid]; !ok {
					continue
				}
			}
			if grouped[cid] == nil {
				grouped[cid] = make(map[spectypes.BeaconRole][]phase0.ValidatorIndex)
			}
			// Populate roles for bits present
			for _, role := range rolemask.All() {
				if rolemask.Has(mask, role) {
					grouped[cid][role] = append(grouped[cid][role], l.ValidatorIndex)
				}
			}
		}
		for cid, roles := range grouped {
			out = append(out, CommitteeScheduleEntry{Slot: slot, CommitteeID: cid, Roles: roles})
		}
	}
	return out
}
