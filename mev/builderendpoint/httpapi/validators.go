package httpapi

import (
	"net/http"
)

func handleValidators(registrar ValidatorRegistrationsForwarderFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if registrar == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		registrationErrors, err := registrar(r.Context(), r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to register validators")
			return
		}

		if len(registrationErrors) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		writeError(w, http.StatusBadRequest, "failed to register validators")
	}
}
