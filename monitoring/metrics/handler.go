package metrics

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	http_pprof "net/http/pprof"
	"runtime"
	"strings"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Handler handles incoming metrics requests
type Handler interface {
	// Start starts an http server, listening to /metrics requests
	Start(mux *http.ServeMux, addr string) error
}

type nodeStatus int32

var (
	metricsNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:node_status",
		Help: "Status of the operator node",
	})
	statusNotHealthy nodeStatus = 0
	statusHealthy    nodeStatus = 1
)

func init() {
	if err := prometheus.Register(metricsNodeStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
}

type metricsHandler struct {
	ctx           context.Context
	logger        *zap.Logger
	db            basedb.IDb
	enableProf    bool
	healthChecker HealthCheckAgent
}

// NewMetricsHandler returns a new metrics handler.
func NewMetricsHandler(ctx context.Context, logger *zap.Logger, db basedb.IDb, enableProf bool, healthChecker HealthCheckAgent) Handler {
	mh := metricsHandler{
		ctx:           ctx,
		logger:        logger.With(zap.String("component", "metrics/handler")),
		db:            db,
		enableProf:    enableProf,
		healthChecker: healthChecker,
	}
	return &mh
}

func (mh *metricsHandler) Start(mux *http.ServeMux, addr string) error {
	mh.logger.Info("setup metrics collection", zap.String("addr", addr),
		zap.Bool("enableProf", mh.enableProf))

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

	// Returns the number of key in the database by collection (hex or string prefix).
	// Empty prefix returns the total number of keys in the database.
	// Example: /database/count-by-collection?prefix=0x1234
	//
	// TODO: re-organize this server and refactor to return proper
	//       JSON responses (also for errors.)
	mux.HandleFunc("/database/count-by-collection", func(w http.ResponseWriter, r *http.Request) {
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

		n, err := mh.db.CountByCollection(prefix)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		response.Count = n

		if err := json.NewEncoder(w).Encode(&response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/health", func(res http.ResponseWriter, req *http.Request) {
		if errs := mh.healthChecker.HealthCheck(); len(errs) > 0 {
			metricsNodeStatus.Set(float64(statusNotHealthy))
			result := map[string][]string{
				"errors": errs,
			}
			if raw, err := json.Marshal(result); err != nil {
				http.Error(res, err.Error(), http.StatusInternalServerError)
			} else {
				http.Error(res, string(raw), http.StatusInternalServerError)
			}
		} else {
			metricsNodeStatus.Set(float64(statusHealthy))
			if _, err := fmt.Fprintln(res, ""); err != nil {
				http.Error(res, err.Error(), http.StatusInternalServerError)
			}
		}
	})

	go func() {
		// TODO: enable lint (G114: Use of net/http serve function that has no support for setting timeouts (gosec))
		// nolint: gosec
		if err := http.ListenAndServe(addr, mux); err != nil {
			mh.logger.Error("failed to start metrics http end-point", zap.Error(err))
		}
	}()

	return nil
}

func (mh *metricsHandler) configureProfiling() {
	runtime.SetBlockProfileRate(1000)
	runtime.SetMutexProfileFraction(1)
}
