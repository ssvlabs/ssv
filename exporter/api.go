package exporter

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"net/http"
)

// OperatorsResponse is the struct that represents an operator
type OperatorsResponse struct {
	Data      []string `json:"data"`
	Timestamp int64    `json:"timestamp"`
}

// ValidatorsResponse is the struct that represents a validator
type ValidatorsResponse struct {
	Data      []string `json:"data"`
	Timestamp int64    `json:"timestamp"`
}

type apiHandlers interface {
	Listen() error
}

type httpHandlers struct {
	mux *http.ServeMux
	e   *exporter

	apiPort int
}

func newHTTPHandlers(e *exporter, apiPort int) apiHandlers {
	mux := http.NewServeMux()
	hh := httpHandlers{mux, e, apiPort}
	hh.registerHandlers()
	return &hh
}

// Listen starts the http server
func (hh *httpHandlers) Listen() error {
	hh.e.logger.Info("exporter - listen for http requests on port " + fmt.Sprintf("%d", hh.apiPort))

	if err := http.ListenAndServe(fmt.Sprintf(":%d", hh.apiPort), hh.mux); err != nil {
		hh.e.logger.Fatal("failed to start exporter http server", zap.Error(err))
		return err
	}
	return nil
}

// register binds http handlers
func (hh *httpHandlers) registerHandlers() {
	hh.mux.HandleFunc("/validators", hh.getAllValidatorsHandler())
	hh.mux.HandleFunc("/operators", hh.getAllOperatorsHandler())
}

// getAllValidatorsHandler returns an http handler for get validators api
func (hh *httpHandlers) getAllValidatorsHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var vr ValidatorsResponse
		err := json.NewEncoder(w).Encode(&vr)
		if err != nil {
			http.Error(w, "Could not parse validators response", http.StatusInternalServerError)
		}
	}
}

// getAllOperatorsHandler returns an http handler for get operators api
func (hh *httpHandlers) getAllOperatorsHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		var or OperatorsResponse
		err := json.NewEncoder(w).Encode(&or)
		if err != nil {
			http.Error(w, "Could not parse operators response", http.StatusInternalServerError)
		}
	}
}
