package httpapi

import (
	"net/http"

	"go.uber.org/zap"
)

func handleValidators(logger *zap.Logger, registrar ValidatorRegistrationsForwarderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if registrar == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		registrationErrors, err := registrar(r.Context(), r.Body)
		if err != nil {
			// Request was invalid (e.g. invalid JSON); keep relay issues best-effort below.
			writeError(w, http.StatusBadRequest, "invalid validator registrations")
			return
		}

		// Best-effort forwarding: do not fail the request when some relays reject registrations.
		// Some beacon clients may treat non-2xx responses as "builder unhealthy" and circuit-break.
		if len(registrationErrors) > 0 && logger != nil {
			logger.Warn("validator registrations forwarding had failures", zap.Int("failures", len(registrationErrors)))
		}

		w.WriteHeader(http.StatusOK)
	}
}
