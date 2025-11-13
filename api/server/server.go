package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	exporterHandlers "github.com/ssvlabs/ssv/api/handlers/exporter"
	nodeHandlers "github.com/ssvlabs/ssv/api/handlers/node"
	pinnedpeers "github.com/ssvlabs/ssv/api/handlers/pinned_peers"
	validatorsHandlers "github.com/ssvlabs/ssv/api/handlers/validators"
	"github.com/ssvlabs/ssv/utils/commons"
)

// Server represents the HTTP API server for SSV.
type Server struct {
	logger *zap.Logger
	addr   string

	node       *nodeHandlers.Node
	pinned     *pinnedpeers.Handler
	validators *validatorsHandlers.Validators
	exporter   *exporterHandlers.Exporter
	httpServer *http.Server

	fullExporter bool
}

// New creates a new Server instance.
func New(
	logger *zap.Logger,
	addr string,
	node *nodeHandlers.Node,
	pinned *pinnedpeers.Handler,
	validators *validatorsHandlers.Validators,
	exporter *exporterHandlers.Exporter,
	fullExporter bool,
) *Server {
	return &Server{
		logger:       logger,
		addr:         addr,
		node:         node,
		pinned:       pinned,
		validators:   validators,
		exporter:     exporter,
		fullExporter: fullExporter,
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

	// @Summary Get node identity information
	// @Description Returns the identity information of the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.NodeIdentityResponse
	// @Router /v1/node/identity [get]
	router.Get("/v1/node/identity", api.Handler(s.node.Identity))

	// @Summary Get connected peers
	// @Description Returns the list of peers connected to the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.PeersResponse
	// @Router /v1/node/peers [get]
	router.Get("/v1/node/peers", api.Handler(s.node.Peers))

	// @Summary Get subscribed topics
	// @Description Returns the list of topics the SSV node is subscribed to
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.TopicsResponse
	// @Router /v1/node/topics [get]
	router.Get("/v1/node/topics", api.Handler(s.node.Topics))

	// @Summary List pinned peers
	// @Tags Node
	// @Produce json
	// @Router /v1/node/pinned-peers [get]
	router.Get("/v1/node/pinned-peers", api.Handler(s.pinned.List))
	// @Summary Add pinned peers
	// @Tags Node
	// @Accept json
	// @Produce json
	// @Router /v1/node/pinned-peers [post]
	router.Post("/v1/node/pinned-peers", api.Handler(s.pinned.Add))
	// @Summary Remove pinned peers
	// @Tags Node
	// @Accept json
	// @Produce json
	// @Router /v1/node/pinned-peers [delete]
	router.Delete("/v1/node/pinned-peers", api.Handler(s.pinned.Remove))

	// @Summary Get node health status
	// @Description Returns the health status of the SSV node
	// @Tags Node
	// @Produce json
	// @Success 200 {object} handlers.HealthResponse
	// @Router /v1/node/health [get]
	router.Get("/v1/node/health", api.Handler(s.node.Health))

	// @Summary Get validators
	// @Description Returns the list of validators managed by the SSV node
	// @Tags Validators
	// @Produce json
	// @Success 200 {object} handlers.ValidatorsResponse
	// @Router /v1/validators [get]
	router.Get("/v1/validators", api.Handler(s.validators.List))

	// We kept both GET and POST methods to ensure compatibility and avoid breaking changes for clients that may rely on either method
	if s.fullExporter {
		router.Post("/v1/exporter/traces/validator", api.Handler(s.exporter.ValidatorTraces))
		router.Get("/v1/exporter/traces/validator", api.Handler(s.exporter.ValidatorTraces))

		router.Post("/v1/exporter/traces/committee", api.Handler(s.exporter.CommitteeTraces))

		router.Get("/v1/exporter/traces/committee", api.Handler(s.exporter.CommitteeTraces))

		router.Get("/v1/exporter/decideds", api.Handler(s.exporter.TraceDecideds))

		router.Post("/v1/exporter/decideds", api.Handler(s.exporter.TraceDecideds))
	} else {
		router.Get("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))

		router.Post("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))
	}

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
