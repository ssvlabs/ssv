package httpapi

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
)

const (
	// EthConsensusVersion is the HTTP response header used by builder endpoints to identify fork version.
	EthConsensusVersion = "Eth-Consensus-Version"
)

type Dependencies struct {
	Logger      *zap.Logger
	BidProvider domain.BidProvider
	Unblinder   domain.Unblinder
	Registrar   domain.RegistrationForwarder
}

// Router is the HTTP transport layer for the Builder API endpoint.
// It intentionally contains no application logic beyond request/response wiring.
type Router struct {
	logger      *zap.Logger
	bidProvider domain.BidProvider
	unblinder   domain.Unblinder
	registrar   domain.RegistrationForwarder
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	rt := &Router{
		logger:      deps.Logger,
		bidProvider: deps.BidProvider,
		unblinder:   deps.Unblinder,
		registrar:   deps.Registrar,
	}

	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(runtime.NumCPU() * 4))
	r.Use(rt.middlewareLogger())

	r.Get("/eth/v1/builder/status", rt.getStatus)
	r.Get("/eth/v1/builder/header/{slot}/{parent_hash}/{pubkey}", rt.getHeader)
	r.Post("/eth/v1/builder/blinded_blocks", rt.postBlindedBlocks)
	r.Post("/eth/v1/builder/validators", rt.postValidators)

	return r
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

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, &apiError{
		Code:    status,
		Message: msg,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
