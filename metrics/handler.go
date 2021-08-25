package metrics

import (
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"log"
	"net/http"
	http_pprof "net/http/pprof"
	"runtime"
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

// NewMetricsHandler creates a new instance
func NewMetricsHandler(logger *zap.Logger, enableProf bool, healthChecker HealthCheckAgent) Handler {
	mh := metricsHandler{
		logger:        logger.With(zap.String("component", "metrics/handler")),
		enableProf:    enableProf,
		healthChecker: healthChecker,
	}
	return &mh
}

type metricsHandler struct {
	logger        *zap.Logger
	enableProf    bool
	healthChecker HealthCheckAgent
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
			if _, err := fmt.Fprintln(res, "{}"); err != nil {
				http.Error(res, err.Error(), http.StatusInternalServerError)
			}
		}
	})

	go func() {
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

