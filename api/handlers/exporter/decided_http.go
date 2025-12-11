package exporter

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/ssvlabs/ssv/api"
)

// TraceDecideds godoc
// @Summary Retrieve decided message traces
// @Description Returns decided duty participant traces for validators or committees, including partial error details.
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query DecidedsRequest false "Filters as query parameters"
// @Param request body DecidedsRequest false "Filters as JSON body"
// @Success 200 {object} TraceDecidedsResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse "Too Many Requests"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/decideds [get]
// @Router /v1/exporter/decideds [post]
func (e *Exporter) TraceDecideds(w http.ResponseWriter, r *http.Request) error {
	// == 1 == Convert HTTP request model to core request model
	var request DecidedsRequest
	if err := api.Bind(r, &request); err != nil {
		return toApiError(e.logger, r, "trace_decideds", http.StatusBadRequest, request, err)
	}
	coreReq, perr := toDecidedsQuery(&request)
	if perr != nil {
		return toApiError(e.logger, r, "trace_decideds", http.StatusBadRequest, request, formatPubKeyLengthError(perr))
	}

	// == 2 == Call core logic
	result, errs := e.svc.TraceDecidedsCore(coreReq)

	// == 3 == Convert core response model to HTTP response model
	if isValidationError(errs) {
		return toApiError(e.logger, r, "trace_decideds", http.StatusBadRequest, request, underlyingValidationError(errs))
	}

	// if we don't have a single valid result and we have at least one meaningful error, return an error
	if len(result.Participants) == 0 && errs.ErrorOrNil() != nil {
		return toApiError(e.logger, r, "trace_decideds", http.StatusInternalServerError, request, errs.ErrorOrNil())
	}

	// otherwise return a partial response with valid participants
	response := TraceDecidedsResponseFromParticipants(result, errs)
	return api.Render(w, r, response)
}

func formatPubKeyLengthError(err *PubKeyLengthError) error {
	return fmt.Errorf("invalid pubkey length: %s", hex.EncodeToString(err.PubKey))
}
