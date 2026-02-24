package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

const (
	// EthConsensusVersion is the HTTP response header used by builder endpoints to identify fork version.
	EthConsensusVersion = "Eth-Consensus-Version"
)

// BidProviderFunc provides Builder API bids (headers) for a given (slot, parent_hash, pubkey).
// Returning (nil, nil) means "no bid available" and should map to HTTP 204.
type BidProviderFunc func(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error)

// UnblinderFunc reveals/unblinds a signed blinded beacon block by calling relays/builders.
type UnblinderFunc func(ctx context.Context, block *api.VersionedSignedBlindedBeaconBlock) (*api.VersionedSignedProposal, error)

// ValidatorRegistrationsForwarderFunc forwards validator registrations to relays/builders.
// It must close the body.
type ValidatorRegistrationsForwarderFunc func(ctx context.Context, body io.ReadCloser) ([]string, error)

// NewRouter creates the Builder API v1 HTTP server.
// It intentionally contains no application logic beyond request/response wiring.
func NewRouter(logger *zap.Logger, bidProvider BidProviderFunc, unblinder UnblinderFunc, registrar ValidatorRegistrationsForwarderFunc) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(runtime.NumCPU() * 4))
	r.Use(middlewareLogger(logger))

	r.Get("/eth/v1/builder/status", handleStatus())
	r.Get("/eth/v1/builder/header/{slot}/{parent_hash}/{pubkey}", handleHeader(bidProvider))
	r.Post("/eth/v1/builder/blinded_blocks", handleBlindedBlocks(unblinder))
	r.Post("/eth/v1/builder/validators", handleValidators(logger, registrar))

	return r
}

func middlewareLogger(logger *zap.Logger) func(next http.Handler) http.Handler {
	if logger == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				logger.Debug(
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
