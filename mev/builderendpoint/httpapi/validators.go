package httpapi

import (
	"net/http"
)

func (rt *Router) postValidators(w http.ResponseWriter, r *http.Request) {
	if rt.registrar == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	registrationErrors, err := rt.registrar.ForwardValidatorRegistrations(r.Context(), r.Body)
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
