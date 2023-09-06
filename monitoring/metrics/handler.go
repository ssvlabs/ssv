package metrics

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	http_pprof "net/http/pprof"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/storage/basedb"
)

// Handler handles incoming metrics requests
type Handler interface {
	// Start starts an http server, listening to /metrics requests
	Start(logger *zap.Logger, mux *http.ServeMux, addr string) error
}

type metricsHandler struct {
	ctx           context.Context
	db            basedb.Database
	reporter      nodeMetrics
	enableProf    bool
	healthChecker HealthChecker
}

// NewMetricsHandler returns a new metrics handler.
func NewMetricsHandler(ctx context.Context, db basedb.Database, reporter nodeMetrics, enableProf bool, healthChecker HealthChecker) Handler {
	if reporter == nil {
		reporter = nopMetrics{}
	}
	mh := metricsHandler{
		ctx:           ctx,
		db:            db,
		reporter:      reporter,
		enableProf:    enableProf,
		healthChecker: healthChecker,
	}
	return &mh
}

func (mh *metricsHandler) Start(logger *zap.Logger, mux *http.ServeMux, addr string) error {
	logger.Info("setup collection", fields.Address(addr), zap.Bool("enableProf", mh.enableProf))

	if mh.enableProf {
		mh.configureProfiling()
		// adding pprof routes manually on an own HTTPMux to avoid lint issue:
		// `G108: Profiling endpoint is automatically exposed on /debug/pprof (gosec)`
		mux.HandleFunc("/debug/pprof/", http_pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", http_pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", http_pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", http_pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", http_pprof.Trace)
	}

	mux.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	mux.HandleFunc("/database/count-by-collection", mh.handleCountByCollection)
	mux.HandleFunc("/health", mh.handleHealth)

	// Set a high timeout to allow for long-running pprof requests.
	const timeout = 600 * time.Second

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	if err := httpServer.ListenAndServe(); err != nil {
		return fmt.Errorf("listen to %s: %w", addr, err)
	}

	return nil
}

// handleCountByCollection responds with the number of key in the database by collection.
// Prefix can be a string or a 0x-prefixed hex string.
// Empty prefix returns the total number of keys in the database.
func (mh *metricsHandler) handleCountByCollection(w http.ResponseWriter, r *http.Request) {
	var response struct {
		Count int64 `json:"count"`
	}

	// Parse prefix from query. Supports both hex and string.
	var prefix []byte
	prefixStr := r.URL.Query().Get("prefix")
	if prefixStr != "" {
		if strings.HasPrefix(prefixStr, "0x") {
			var err error
			prefix, err = hex.DecodeString(prefixStr[2:])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			prefix = []byte(prefixStr)
		}
	}

	n, err := mh.db.CountPrefix(prefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Count = n

	if err := json.NewEncoder(w).Encode(&response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (mh *metricsHandler) handleHealth(res http.ResponseWriter, req *http.Request) {
	if err := mh.healthChecker.HealthCheck(); err != nil {
		mh.reporter.SSVNodeNotHealthy()
		result := map[string][]string{
			"errors": {err.Error()},
		}
		if raw, err := json.Marshal(result); err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(res, string(raw), http.StatusInternalServerError)
		}
	} else {
		mh.reporter.SSVNodeHealthy()
		if _, err := fmt.Fprintln(res, ""); err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (mh *metricsHandler) configureProfiling() {
	runtime.SetBlockProfileRate(10000)
	runtime.SetMutexProfileFraction(5)
}
