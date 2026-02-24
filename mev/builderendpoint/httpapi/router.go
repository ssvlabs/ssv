package httpapi

import (
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// Router is the HTTP transport layer for the Builder API endpoint.
// It intentionally contains no application logic beyond request/response wiring.
type Router struct {
	logger *zap.Logger
}

func NewRouter(logger *zap.Logger) http.Handler {
	r := chi.NewRouter()

	rt := &Router{logger: logger}

	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(runtime.NumCPU() * 4))
	r.Use(rt.middlewareLogger())

	r.Get("/eth/v1/builder/status", rt.getStatus)

	return r
}

func (rt *Router) getStatus(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (rt *Router) middlewareLogger() func(next http.Handler) http.Handler {
	if rt.logger == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				rt.logger.Debug(
					"served builder endpoint request",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Int64("request_length", r.ContentLength),
					zap.Int("response_length", ww.BytesWritten()),
					zap.Duration("took", time.Since(start)),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}
