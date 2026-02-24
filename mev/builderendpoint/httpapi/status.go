package httpapi

import "net/http"

func (rt *Router) getStatus(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
