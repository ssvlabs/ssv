package exporter

import (
	"errors"
	"net/http"

	"github.com/hashicorp/go-multierror"

	"github.com/ssvlabs/ssv/api"
	exportercore "github.com/ssvlabs/ssv/exporter"
)

// ValidatorTraces godoc
// @Summary Retrieve validator duty traces
// @Description Returns consensus, decided, and message traces for the requested validator duties.
// @Description For AGGREGATOR and SYNC_COMMITTEE_CONTRIBUTION the fork state is evaluated at 'from': a range whose
// @Description 'from' is post-Boole and that supplies no 'pubkeys'/'indices' is rejected with 400, while a range whose
// @Description 'from' is pre-Boole is accepted and served partially — post-Boole slots are omitted from 'data' and
// @Description reported as one note per role in 'errors' with the text "committee duty post-fork". Such a response is
// @Description a 200 even when 'data' is empty; clients must inspect 'errors' to detect partial coverage, and should
// @Description supply 'indices'/'pubkeys' or use /traces/committee to retrieve the post-fork portion.
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query ValidatorTracesRequest false "Filters as query parameters"
// @Param request body ValidatorTracesRequest false "Filters as JSON body"
// @Success 200 {object} ValidatorTracesResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse "Too Many Requests"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/traces/validator [get]
// @Router /v1/exporter/traces/validator [post]
func (e *Exporter) ValidatorTraces(w http.ResponseWriter, r *http.Request) error {
	// == 1 == Convert HTTP request model to core request model
	var request ValidatorTracesRequest
	if err := api.Bind(r, &request); err != nil {
		return toApiError(e.logger, r, "validator_traces", http.StatusBadRequest, request, err)
	}
	coreReq, perr := toValidatorTracesQuery(&request)
	if perr != nil {
		return toApiError(e.logger, r, "validator_traces", http.StatusBadRequest, request, formatPubKeyLengthError(perr))
	}

	// == 2 == Call core logic
	result, errs := e.svc.ValidatorTracesCore(coreReq)

	// == 3 == Convert core response model to HTTP response model
	if isValidationError(errs) {
		return toApiError(e.logger, r, "validator_traces", http.StatusBadRequest, request, underlyingValidationError(errs))
	}

	// if we don't have a single valid result and we have at least one meaningful error, return an error.
	// post-fork committee-duty notes are expected on fork-straddling ranges whose pre-fork slots
	// yield no traces (e.g. sparse aggregator duties), so they don't count as a hard failure here.
	if len(result.Traces) == 0 && errs.ErrorOrNil() != nil && !onlyPostForkCommitteeDutyNotes(errs) {
		return toApiError(e.logger, r, "validator_traces", http.StatusInternalServerError, request, errs.ErrorOrNil())
	}

	// otherwise return a partial response with valid duties
	response := toValidatorTraceResponse(result, errs)
	return api.Render(w, r, response)
}

// onlyPostForkCommitteeDutyNotes reports whether every error in errs is (or wraps)
// exportercore.ErrPostForkCommitteeDutyNote, i.e. the errors are non-fatal notes
// rather than genuine processing failures.
func onlyPostForkCommitteeDutyNotes(errs *multierror.Error) bool {
	if errs.ErrorOrNil() == nil {
		return false
	}
	for _, err := range errs.Errors {
		if !errors.Is(err, exportercore.ErrPostForkCommitteeDutyNote) {
			return false
		}
	}
	return true
}
