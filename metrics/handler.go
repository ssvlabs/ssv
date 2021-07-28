package metrics

import (
	"fmt"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
	http_pprof "net/http/pprof"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
)

// Handler handles incoming metrics requests
type Handler interface {
	// Start starts an http server, listening to /metrics requests
	Start(mux *http.ServeMux, addr string) error
}

// NewMetricsHandler creates a new instance
func NewMetricsHandler(logger *zap.Logger) Handler {
	mh := metricsHandler{
		logger: logger.With(zap.String("component", "metrics/handler")),
	}
	return &mh
}

type metricsHandler struct {
	logger *zap.Logger
}

func (mh *metricsHandler) Start(mux *http.ServeMux, addr string) error {
	mh.logger.Info("setup metrics collection")

	mh.configure()

	mux.HandleFunc("/metrics", func(res http.ResponseWriter, req *http.Request) {
		if err := mh.handleHTTP(res, req); err != nil {
			// TODO: decide if we want to ignore errors
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	})

	// adding pprof routes manually on an own HTTPMux to avoid lint issue:
	// `G108: Profiling endpoint is automatically exposed on /debug/pprof (gosec)`
	mux.HandleFunc("/debug/pprof/", http_pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", http_pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", http_pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", http_pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", http_pprof.Trace)

	mux.HandleFunc("/goroutines", func(res http.ResponseWriter, _ *http.Request) {
		stack := debug.Stack()
		if _, err := res.Write(stack); err != nil {
			mh.logger.Error("failed to write goroutines stack", zap.Error(err))
		}
		if err := pprof.Lookup("goroutine").WriteTo(res, 2); err != nil {
			mh.logger.Error("failed to write pprof goroutines", zap.Error(err))
		}
	})

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			mh.logger.Error("failed to start metrics http end-point", zap.Error(err))
		}
	}()

	return nil
}

func (mh *metricsHandler) configure() {
	runtime.SetBlockProfileRate(1000)
	runtime.SetMutexProfileFraction(1)
}

func (mh *metricsHandler) handleHTTP(res http.ResponseWriter, _ *http.Request) (err error) {
	var metrics []string
	var errs []error
	if metrics, errs = Collect(); len(errs) > 0 {
		mh.logger.Error("failed to collect metrics", zap.Errors("metricsCollectErrs", errs))
		return errors.New("failed to collect metrics")
	}
	if _, err = fmt.Fprintln(res, strings.Join(metrics, "\n")); err != nil {
		mh.logger.Error("failed to send metrics", zap.Error(err))
	}
	return err
}
