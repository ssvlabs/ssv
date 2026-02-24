package httpapi

import "net/http"

func handleStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}
