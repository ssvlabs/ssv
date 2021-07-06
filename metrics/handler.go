package metrics

import (
	"fmt"
	"go.uber.org/zap"
	"net/http"
	"strings"
)

// Handler handles incoming metrics requests
type Handler interface {
	// Start starts an http server, listening to metrics requests
	Start(mux *http.ServeMux, addr string) error
}

// NewMetricsHandler creates a new instance
func NewMetricsHandler(logger *zap.Logger, collector Collector) Handler {
	mh := metricsHandler{logger.With(zap.String("component", "metrics/handler")), collector}
	return &mh
}

type metricsHandler struct {
	logger    *zap.Logger
	collector Collector
}

func (mh *metricsHandler) Start(mux *http.ServeMux, addr string) error {
	mux.HandleFunc("/metrics", func(res http.ResponseWriter, req *http.Request) {
		if err := mh.handleHTTP(res, req); err != nil {
			// TODO handle errors
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	})

	if err := http.ListenAndServe(addr, mux); err != nil {
		mh.logger.Error("failed to start metrics http end-point", zap.Error(err))
		return err
	}
	return nil
}

func (mh *metricsHandler) handleHTTP(res http.ResponseWriter, req *http.Request) (err error) {
	var metrics []string
	if metrics, err = mh.collector.Collect(); err != nil {
		mh.logger.Error("failed to collect metrics", zap.Error(err))
		return err
	}
	if _, err = fmt.Fprintln(res, strings.Join(metrics, "\n")); err != nil {
		mh.logger.Error("failed to send metrics", zap.Error(err))
	}
	return err
}
