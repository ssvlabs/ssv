package exporter

import (
	"net/http"

	"github.com/ssvlabs/ssv/api"
)

// CommitteeTraces godoc
// @Summary Retrieve committee duty traces
// @Description Returns consensus and post-consensus traces for requested committees.
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query CommitteeTracesRequest false "Filters as query parameters"
// @Param request body CommitteeTracesRequest false "Filters as JSON body"
// @Success 200 {object} CommitteeTracesResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse "Too Many Requests"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/traces/committee [get]
// @Router /v1/exporter/traces/committee [post]
func (e *Exporter) CommitteeTraces(w http.ResponseWriter, r *http.Request) error {
	// == 1 == Convert HTTP request model to core request model
	var request CommitteeTracesRequest
	if err := api.Bind(r, &request); err != nil {
		return toApiError(e.logger, r, "committee_traces", http.StatusBadRequest, request, err)
	}

	// == 2 == Call core logic
	duties, schedule, errs := e.CommitteeTracesCore(&request)

	// == 3 == Convert core response model to HTTP response model
	if isValidationError(errs) {
		return toApiError(e.logger, r, "committee_traces", http.StatusBadRequest, request, underlyingValidationError(errs))
	}

	// if we don't have a single valid result and we have at least one meaningful error, return an error
	if len(duties) == 0 && errs.ErrorOrNil() != nil {
		return toApiError(e.logger, r, "committee_traces", http.StatusInternalServerError, request, errs.ErrorOrNil())
	}

	// otherwise return a partial response with valid duties
	resp := toCommitteeTraceResponse(duties, errs)
	resp.Schedule = schedule
	return api.Render(w, r, resp)
}
