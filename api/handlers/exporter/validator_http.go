package exporter

import (
	"net/http"

	"github.com/ssvlabs/ssv/api"
)

// ValidatorTraces godoc
// @Summary Retrieve validator duty traces
// @Description Returns consensus, decided, and message traces for the requested validator duties.
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

	// if we don't have a single valid result and we have at least one meaningful error, return an error
	if len(result.Traces) == 0 && errs.ErrorOrNil() != nil {
		return toApiError(e.logger, r, "validator_traces", http.StatusInternalServerError, request, errs.ErrorOrNil())
	}

	// otherwise return a partial response with valid duties
	response := toValidatorTraceResponse(result, errs)
	return api.Render(w, r, response)
}
