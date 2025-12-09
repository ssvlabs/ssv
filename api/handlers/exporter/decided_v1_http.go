package exporter

import (
	"net/http"

	"github.com/ssvlabs/ssv/api"
)

// Decideds is the backward-compatible handler for exporter-v1 "decideds" endpoint.
// It adapts HTTP requests to the DecidedsCore business logic.
func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	// == 1 == Convert HTTP request model to core request model
	var request DecidedsRequest
	if err := api.Bind(r, &request); err != nil {
		return toApiError(e.logger, r, "decideds_v1", http.StatusBadRequest, request, err)
	}
	coreReq, perr := toDecidedsQuery(&request)
	if perr != nil {
		return toApiError(e.logger, r, "decideds_v1", http.StatusBadRequest, request, formatPubKeyLengthError(perr))
	}

	// == 2 == Call core logic
	result, err := e.svc.DecidedsCore(coreReq)

	// == 3 == Convert core response model to HTTP response model
	if err != nil {
		if isValidationError(err) {
			return toApiError(e.logger, r, "decideds_v1", http.StatusBadRequest, request, underlyingValidationError(err))
		}
		return toApiError(e.logger, r, "decideds_v1", http.StatusInternalServerError, request, err)
	}

	response := DecidedsResponseFromResult(result)
	return api.Render(w, r, response)
}
