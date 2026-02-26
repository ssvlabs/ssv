package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func handleValidators(logger *zap.Logger, registrar ValidatorRegistrationsForwarderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if registrar == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		contentType := codec.NormalizeContentType(r.Header.Get("Content-Type"))

		registrations, err := codec.UnmarshalValidatorRegistrations(contentType, r.Body)
		if err != nil {
			var unsupported codec.UnsupportedContentTypeError
			if errors.As(err, &unsupported) {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported content type")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid validator registrations")
			return
		}

		registrationErrors, err := registrar(r.Context(), registrations)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to forward validator registrations")
			return
		}

		// Best-effort forwarding: do not fail the request when some relays reject registrations.
		// Some beacon clients may treat non-2xx responses as "builder unhealthy" and circuit-break.
		if len(registrationErrors) > 0 && logger != nil {
			failedRelays := make([]string, 0, len(registrationErrors))
			for _, failure := range registrationErrors {
				relay, _, ok := strings.Cut(failure, ": ")
				if ok && relay != "" {
					failedRelays = append(failedRelays, relay)
				}
			}

			logger.Warn(
				"validator registrations forwarding had failures",
				zap.Int("failures", len(registrationErrors)),
				zap.Strings("failed_relays", failedRelays),
				zap.Strings("relay_failures", registrationErrors),
			)
		}

		w.WriteHeader(http.StatusOK)
	}
}
