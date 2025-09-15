package exporter

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

func (e *Exporter) CommitteeTraces(w http.ResponseWriter, r *http.Request) error {
	var request committeeRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateCommitteeRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	var all []*exporter.CommitteeDutyTrace
	var errs *multierror.Error
	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		duties, err := e.getCommitteeDutiesForSlot(slot, request.parseCommitteeIds())
		all = append(all, duties...)
		errs = multierror.Append(errs, err)
	}
	return api.Render(w, r, toCommitteeTraceResponse(all, errs))
}

func validateCommitteeRequest(request *committeeRequest) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}

	requiredLength := len(spectypes.CommitteeID{})
	for _, cmt := range request.CommitteeIDs {
		if len(cmt) != requiredLength {
			return fmt.Errorf("invalid committee ID length: %s", hex.EncodeToString(cmt))
		}
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
			// if error is not found, nothing to report as we might not have a duty for this role
			// otherwise report it:
			if !isNotFoundError(err) {
				e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.CommitteeID(cmtID))
				errs = multierror.Append(errs, err)
			}
			continue
		}
		duties = append(duties, duty)
	}
	return duties, errs.ErrorOrNil()
}

func toCommitteeTraceResponse(duties []*exporter.CommitteeDutyTrace, errs *multierror.Error) *committeeTraceResponse {
	r := new(committeeTraceResponse)
	r.Data = make([]committeeTrace, 0)
	for _, t := range duties {
		r.Data = append(r.Data, toCommitteeTrace(t))
	}
	if errs != nil {
		r.Errors = toStrings(errs.Errors)
	}
	return r
}
