package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/api"
	"github.com/bloxapp/ssv/api/handlers"
	"github.com/bloxapp/ssv/utils/commons"
)

type Server struct {
	logger *zap.Logger
	addr   string

	node       *handlers.Node
	validators *handlers.Validators
	exporter   *handlers.Exporter
}

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
	router.Get("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))
	router.Post("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))

	s.logger.Info("Serving SSV API", zap.String("addr", s.addr))

	server := &http.Server{
		Addr:         s.addr,
		Handler:      router,
		ReadTimeout:  12 * time.Second,
		WriteTimeout: 12 * time.Second,
	}
	return server.ListenAndServe()
}

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

func middlewareNodeVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SSV-Node-Version", commons.GetNodeVersion())
		next.ServeHTTP(w, r)
	})
}
