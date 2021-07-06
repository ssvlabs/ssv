package metrics

import (
	"fmt"
	"go.uber.org/zap"
	"net/http"
	"strings"
)

type MetricsHandler interface {
	HandleHTTP(res http.ResponseWriter, req *http.Request) error
	Start(mux *http.ServeMux, addr string) error
}

func NewMetricsHandler(logger *zap.Logger, collector Collector) MetricsHandler {
	mh := metricsHandler{logger.With(zap.String("component", "metrics/handler")), collector}
	return &mh
}

type metricsHandler struct {
	logger *zap.Logger
	collector Collector
}

func (mh *metricsHandler) Start(mux *http.ServeMux, addr string) error {
	mux.HandleFunc("/metrics", func(res http.ResponseWriter, req *http.Request) {
		if err := mh.HandleHTTP(res, req); err != nil {
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

func (mh *metricsHandler) HandleHTTP(res http.ResponseWriter, req *http.Request) (err error) {
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

