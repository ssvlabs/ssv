package metrics

import (
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

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type (
	// healthChecker represent an health-check agent
	healthChecker interface {
		HealthCheck() error
	}
	// Handler serves diagnostic and metrics endpoints such as Prometheus metrics, health checks
	// and profiling endpoints (pprof).
	Handler struct {
		logger        *zap.Logger
		db            basedb.Database
		enableProf    bool
		healthChecker healthChecker
	}
)

// NewHandler returns a new metrics handler.
func NewHandler(logger *zap.Logger, db basedb.Database, enableProf bool, healthChecker healthChecker) *Handler {
	mh := Handler{
		logger:        logger,
		db:            db,
		enableProf:    enableProf,
		healthChecker: healthChecker,
	}
	return &mh
}

func (h *Handler) Start(mux *http.ServeMux, addr string) error {
	h.logger.Info("setup collection", fields.Address(addr), zap.Bool("enableProf", h.enableProf))

	if h.enableProf {
		h.configureProfiling()
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
	mux.HandleFunc("/database/count-by-collection", h.handleCountByCollection)
	mux.HandleFunc("/health", h.handleHealth)

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
func (h *Handler) handleCountByCollection(w http.ResponseWriter, r *http.Request) {
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

	n, err := h.db.CountPrefix(prefix)
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

func (h *Handler) handleHealth(res http.ResponseWriter, req *http.Request) {
	if err := h.healthChecker.HealthCheck(); err != nil {
		result := map[string][]string{
			"errors": {err.Error()},
		}
		if raw, err := json.Marshal(result); err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(res, string(raw), http.StatusInternalServerError)
		}
	} else {
		if _, err := fmt.Fprintln(res, ""); err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (h *Handler) configureProfiling() {
	runtime.SetBlockProfileRate(10000)
	runtime.SetMutexProfileFraction(5)
}
