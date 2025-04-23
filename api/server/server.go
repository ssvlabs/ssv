package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/api/handlers"
	"github.com/ssvlabs/ssv/utils/commons"
)

// Server represents the HTTP API server for SSV.
type Server struct {
	logger *zap.Logger
	addr   string

	node       *handlers.Node
	validators *handlers.Validators
	exporter   *handlers.Exporter

	httpServer *http.Server
}

// New creates a new Server instance.
func New(
	logger *zap.Logger,
	addr string,
	node *handlers.Node,
	validators *handlers.Validators,
	exporter *handlers.Exporter,
) *Server {
	return &Server{
		logger:     logger,
		addr:       addr,
		node:       node,
		validators: validators,
		exporter:   exporter,
	}
}

// Run starts the server and blocks until it's shut down.
func (s *Server) Run() error {
	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(middleware.Throttle(runtime.NumCPU() * 4))
	router.Use(middleware.Compress(5, "application/json"))
	router.Use(middlewareLogger(s.logger))
	router.Use(middlewareNodeVersion)

	router.Get("/v1/node/identity", api.Handler(s.node.Identity))
	router.Get("/v1/node/peers", api.Handler(s.node.Peers))
	router.Get("/v1/node/topics", api.Handler(s.node.Topics))
	router.Get("/v1/node/health", api.Handler(s.node.Health))
	router.Get("/v1/validators", api.Handler(s.validators.List))

	// We kept both GET and POST methods to ensure compatibility and avoid breaking changes for clients that may rely on either method
	router.Get("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))
	router.Post("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))

	s.logger.Info("Serving SSV API", zap.String("addr", s.addr))

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       12 * time.Second,
		WriteTimeout:      12 * time.Second,
	}
	return s.httpServer.ListenAndServe()
}

// middlewareLogger creates a middleware that logs API requests.
func middlewareLogger(logger *zap.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				logger.Debug(
					"served SSV API request",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Int64("request_length", r.ContentLength),
					zap.Int("response_length", ww.BytesWritten()),
					zap.Duration("took", time.Since(start)),
				)
			}()
			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}

// middlewareNodeVersion adds the SSV node version as a response header.
func middlewareNodeVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SSV-Node-Version", commons.GetNodeVersion())
		next.ServeHTTP(w, r)
	})
}
