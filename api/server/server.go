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

	fullExporter bool
}

// New creates a new Server instance.
func New(
	logger *zap.Logger,
	addr string,
	node *handlers.Node,
	validators *handlers.Validators,
	exporter *handlers.Exporter,
	fullExporter bool,
) *Server {
	return &Server{
		logger:       logger,
		addr:         addr,
		node:         node,
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
		// @Summary Post validator traces
		// @Description Returns traces for a specific validator
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.ValidatorTracesResponse
		// @Router /v1/exporter/traces/validator [post]
		router.Post("/v1/exporter/traces/validator", api.Handler(s.exporter.ValidatorTraces))
		// @Summary Get validator traces
		// @Description Returns traces for a specific validator
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.ValidatorTracesResponse
		// @Router /v1/exporter/traces/validator [get]
		router.Get("/v1/exporter/traces/validator", api.Handler(s.exporter.ValidatorTraces))

		// @Summary Post committee traces
		// @Description Returns traces for a specific committee
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.CommitteeTracesResponse
		// @Router /v1/exporter/traces/committee [post]
		router.Post("/v1/exporter/traces/committee", api.Handler(s.exporter.CommitteeTraces))

		// @Summary Get committee traces
		// @Description Returns traces for a specific committee
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.CommitteeTracesResponse
		// @Router /v1/exporter/traces/committee [get]
		router.Get("/v1/exporter/traces/committee", api.Handler(s.exporter.CommitteeTraces))

		// @Summary Get decided messages traces
		// @Description Returns traces of decided messages
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.TraceDecidedsResponse
		// @Router /v1/exporter/decideds [get]
		router.Get("/v1/exporter/decideds", api.Handler(s.exporter.TraceDecideds))

		// @Summary Get decided messages traces
		// @Description Returns traces of decided messages
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.TraceDecidedsResponse
		// @Router /v1/exporter/decideds [post]
		router.Post("/v1/exporter/decideds", api.Handler(s.exporter.TraceDecideds))
	} else {
		// @Summary Get decided messages
		// @Description Returns decided messages
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.DecidedsResponse
		// @Router /v1/exporter/decideds [get]
		router.Get("/v1/exporter/decideds", api.Handler(s.exporter.Decideds))

		// @Summary Get decided messages
		// @Description Returns decided messages
		// @Tags Exporter
		// @Produce json
		// @Success 200 {object} handlers.DecidedsResponse
		// @Router /v1/exporter/decideds [post]
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
