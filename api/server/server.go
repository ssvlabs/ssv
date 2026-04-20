package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	exporterHandlers "github.com/ssvlabs/ssv/api/handlers/exporter"
	nodeHandlers "github.com/ssvlabs/ssv/api/handlers/node"
	validatorsHandlers "github.com/ssvlabs/ssv/api/handlers/validators"
	"github.com/ssvlabs/ssv/utils/commons"
)

// Server represents the HTTP API server for SSV.
type Server struct {
	logger *zap.Logger

	addr   string
	router *chi.Mux
}

// New creates a new Server instance.
func New(
	logger *zap.Logger,
	addr string,
	node *nodeHandlers.Node,
	validators *validatorsHandlers.Validators,
	exporter *exporterHandlers.Exporter,
	fullExporter bool,
) *Server {
	router := chi.NewRouter()

	router.Use(middleware.Recoverer)
	router.Use(middleware.Throttle(runtime.NumCPU() * 4))
	router.Use(middleware.Compress(5, "application/json"))
	router.Use(middlewareLogger(logger))
	router.Use(middlewareNodeVersion)

	// @Summary Get node identity information
	// @Description Returns the identity information of the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.NodeIdentityResponse
	// @Router /v1/node/identity [get]
	router.Get("/v1/node/identity", api.Handler(node.Identity))

	// @Summary Get connected peers
	// @Description Returns the list of peers connected to the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.PeersResponse
	// @Router /v1/node/peers [get]
	router.Get("/v1/node/peers", api.Handler(node.Peers))

	// @Summary Get subscribed topics
	// @Description Returns the list of topics the SSV node is subscribed to
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.TopicsResponse
	// @Router /v1/node/topics [get]
	router.Get("/v1/node/topics", api.Handler(node.Topics))

	// @Summary Get node health status
	// @Description Returns the health status of the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.HealthResponse
	// @Router /v1/node/health [get]
	router.Get("/v1/node/health", api.Handler(node.Health))

	// @Summary Get validators
	// @Description Returns the list of validators managed by the SSV node
	// @Tags Validators
	// @Produce json
	// @Success 200 {object} handlers.ValidatorsResponse
	// @Router /v1/validators [get]
	router.Get("/v1/validators", api.Handler(validators.List))

	// We kept both GET and POST methods to ensure compatibility and avoid breaking changes for clients that may rely on either method
	if fullExporter {
		router.Post("/v1/exporter/traces/validator", api.Handler(exporter.ValidatorTraces))
		router.Get("/v1/exporter/traces/validator", api.Handler(exporter.ValidatorTraces))

		router.Post("/v1/exporter/traces/committee", api.Handler(exporter.CommitteeTraces))

		router.Get("/v1/exporter/traces/committee", api.Handler(exporter.CommitteeTraces))

		router.Get("/v1/exporter/decideds", api.Handler(exporter.TraceDecideds))

		router.Post("/v1/exporter/decideds", api.Handler(exporter.TraceDecideds))
	} else {
		router.Get("/v1/exporter/decideds", api.Handler(exporter.Decideds))

		router.Post("/v1/exporter/decideds", api.Handler(exporter.Decideds))
	}

	return &Server{
		logger: logger,
		addr:   addr,
		router: router,
	}
}

// Start binds a listener on the configured address and serves the HTTP API in
// the background. It returns the bound address once the listener is ready (so
// callers using a :0 port can discover the kernel-assigned one). The server
// shuts down when ctx is canceled.
func (s *Server) Start(ctx context.Context) (string, error) {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	boundAddr := l.Addr().String()

	httpServer := &http.Server{
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       12 * time.Second,
		WriteTimeout:      12 * time.Second,
	}

	s.logger.Info("Serving SSV API", zap.String("addr", boundAddr))

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("shutdown failed", zap.Error(err))
		}
	}()

	go func() {
		if err := httpServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve loop exited", zap.Error(err))
		}
	}()

	return boundAddr, nil
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

// middlewareNodeVersion adds the SSV node version as a response header.
func middlewareNodeVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SSV-Node-Version", commons.GetNodeVersion())
		next.ServeHTTP(w, r)
	})
}
