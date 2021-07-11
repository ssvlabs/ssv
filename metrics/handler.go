package metrics

import (
	"fmt"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
	_ "net/http/pprof"
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

	mux.HandleFunc("/metrics", func(res http.ResponseWriter, req *http.Request) {
		if err := mh.handleHTTP(res, req); err != nil {
			// TODO: decide if we want to ignore errors
			http.Error(res, err.Error(), http.StatusInternalServerError)
		}
	})

	//http.HandleFunc("/debug/pprof/", Index)
	//http.HandleFunc("/debug/pprof/cmdline", Cmdline)
	//http.HandleFunc("/debug/pprof/profile", Profile)
	//http.HandleFunc("/debug/pprof/symbol", Symbol)
	//http.HandleFunc("/debug/pprof/trace", Trace)
	//
	//mux.HandleFunc("/prof", func (res http.ResponseWriter, req *http.Request) {
	//	req.Header.Get("")
	//})

	mux.HandleFunc("/goroutines", func (res http.ResponseWriter, _ *http.Request) {
		stack := debug.Stack()
		if _, err := res.Write(stack); err != nil {
			mh.logger.Error("failed to write goroutines stack", zap.Error(err))
		}
		if err := pprof.Lookup("goroutine").WriteTo(res, 2); err != nil {
			mh.logger.Error("failed to write pprof goroutines", zap.Error(err))
		}
	})

	if err := http.ListenAndServe(addr, mux); err != nil {
		mh.logger.Error("failed to start metrics http end-point", zap.Error(err))
		return err
	}
	return nil
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
