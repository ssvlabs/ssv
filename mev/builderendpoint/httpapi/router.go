package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec/phase0"

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
}

// Router is the HTTP transport layer for the Builder API endpoint.
// It intentionally contains no application logic beyond request/response wiring.
type Router struct {
	logger      *zap.Logger
	bidProvider domain.BidProvider
	unblinder   domain.Unblinder
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	rt := &Router{
		logger:      deps.Logger,
		bidProvider: deps.BidProvider,
		unblinder:   deps.Unblinder,
	}

	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(runtime.NumCPU() * 4))
	r.Use(rt.middlewareLogger())

	r.Get("/eth/v1/builder/status", rt.getStatus)
	r.Get("/eth/v1/builder/header/{slot}/{parent_hash}/{pubkey}", rt.getHeader)
	r.Post("/eth/v1/builder/blinded_blocks", rt.postBlindedBlocks)

	return r
}

func (rt *Router) getStatus(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (rt *Router) getHeader(w http.ResponseWriter, r *http.Request) {
	if rt.bidProvider == nil {
		// Not configured yet; behave as "no bid" rather than hanging or failing.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	slot, parentHash, pubkey, err := parseHeaderParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	bid, err := rt.bidProvider.BuilderBid(r.Context(), slot, parentHash, pubkey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to obtain bid")
		return
	}

	if bid == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set(EthConsensusVersion, bid.Version.String())
	writeJSON(w, http.StatusOK, bid)
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

func parseHeaderParams(r *http.Request) (phase0.Slot, phase0.Hash32, phase0.BLSPubKey, error) {
	slotStr := chi.URLParam(r, "slot")
	parentHashStr := chi.URLParam(r, "parent_hash")
	pubkeyStr := chi.URLParam(r, "pubkey")

	slotU64, err := strconv.ParseUint(slotStr, 10, 64)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid slot")
	}
	slot := phase0.Slot(slotU64)

	parentHash, err := parseHexBytes32(parentHashStr)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid parent_hash")
	}

	pubkey, err := parseHexBytes48(pubkeyStr)
	if err != nil {
		return 0, phase0.Hash32{}, phase0.BLSPubKey{}, fmt.Errorf("invalid pubkey")
	}

	return slot, parentHash, pubkey, nil
}

func parseHexBytes32(input string) (phase0.Hash32, error) {
	raw, err := parseFixedHex(input, 32)
	if err != nil {
		return phase0.Hash32{}, err
	}
	var out phase0.Hash32
	copy(out[:], raw)
	return out, nil
}

func parseHexBytes48(input string) (phase0.BLSPubKey, error) {
	raw, err := parseFixedHex(input, 48)
	if err != nil {
		return phase0.BLSPubKey{}, err
	}
	var out phase0.BLSPubKey
	copy(out[:], raw)
	return out, nil
}

func parseFixedHex(input string, size int) ([]byte, error) {
	trimmed := strings.TrimPrefix(input, "0x")
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	if len(b) != size {
		return nil, fmt.Errorf("expected %d bytes, got %d", size, len(b))
	}
	return b, nil
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
