package beaconproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/auto"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/beacon_proxy/intercept"
)

type (
	// GatewayKey is the key used to store the gateway in the request context.
	GatewayKey struct{}

	// LoggerKey is the key used to store the logger in the request context.
	LoggerKey struct{}

	// StartTimeKey is the key used to store the start time in the request context.
	StartTimeKey struct{}
)

type Gateway struct {
	Name        string
	Port        int
	Interceptor intercept.Interceptor
}

type BeaconProxy struct {
	remote   *url.URL
	client   eth2client.Service
	proxy    *httputil.ReverseProxy
	logger   *zap.Logger
	gateways map[int]Gateway
}

func New(
	ctx context.Context,
	logger *zap.Logger,
	remoteAddr string,
	gateways []Gateway,
) (*BeaconProxy, error) {
	client, err := auto.New(
		ctx,
		auto.WithAddress(remoteAddr),
		auto.WithTimeout(30*time.Second),
		auto.WithLogLevel(zerolog.ErrorLevel),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to remote beacon node: %w", err)
	}
	remote, err := url.Parse(remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote beacon address: %w", err)
	}
	b := &BeaconProxy{
		remote:   remote,
		proxy:    httputil.NewSingleHostReverseProxy(remote),
		client:   client,
		logger:   logger,
		gateways: make(map[int]Gateway),
	}
	for _, gateway := range gateways {
		b.gateways[gateway.Port] = gateway
	}
	return b, nil
}

func (b *BeaconProxy) Run(ctx context.Context) error {
	// Serve each endpoint on a separate port.
	pool := pool.New().WithContext(ctx)
	for port, gateway := range b.gateways {
		b.logger.Debug("starting proxy server",
			zap.String("gateway", gateway.Name),
			zap.Int("port", port),
		)
		addr := fmt.Sprintf(":%d", port)
		pool.Go(func(ctx context.Context) error {
			server := http.Server{
				Addr:         addr,
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 30 * time.Second,
				IdleTimeout:  30 * time.Second,
				Handler:      b.router(gateway),
			}
			return server.ListenAndServe()
		})
	}

	return pool.Wait()
}

func (b *BeaconProxy) router(gateway Gateway) *chi.Mux {
	r := chi.NewRouter()
	r.Use(b.middleware(gateway))

	// Attestations.
	r.HandleFunc("/eth/v1/validator/duties/attester/{epoch}", b.handleAttesterDuties)
	r.HandleFunc("/eth/v1/validator/attestation_data", b.handleAttestationData)
	r.HandleFunc("/eth/v1/beacon/pool/attestations", b.handleSubmitAttestations)

	// Proposals.
	r.HandleFunc("/eth/v1/validator/duties/proposer/{epoch}", b.handleProposerDuties)
	r.HandleFunc("/eth/v2/validator/blocks/{slot}", b.handleBlockProposal)
	r.HandleFunc("/eth/v1/beacon/blocks", b.handleSubmitBlockProposal)

	// Passthrough everything else.
	r.HandleFunc("/*", b.passthrough)

	return r
}

func (b *BeaconProxy) middleware(gateway Gateway) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add gateway to context.
			ctx := context.WithValue(r.Context(), GatewayKey{}, gateway)

			// Add logger to context.
			logger := b.logger.With(
				zap.String("gateway", gateway.Name),
				zap.String("endpoint", fmt.Sprintf("%s %s", r.Method, r.URL.Path)),
			)
			ctx = context.WithValue(ctx, LoggerKey{}, logger)

			ctx = context.WithValue(ctx, StartTimeKey{}, time.Now())

			// Log request.
			b.logger.Debug("received request",
				zap.String("gateway", gateway.Name),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("query", r.URL.Query().Encode()),
			)

			// Call next handler.
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (b *BeaconProxy) passthrough(w http.ResponseWriter, r *http.Request) {
	r.Host = b.remote.Host
	b.proxy.ServeHTTP(w, r)
}

func (b *BeaconProxy) requestContext(r *http.Request) (*zap.Logger, Gateway) {
	return r.Context().Value(LoggerKey{}).(*zap.Logger),
		r.Context().Value(GatewayKey{}).(Gateway)
}

func (b *BeaconProxy) error(r *http.Request, w http.ResponseWriter, status int, err error) {
	logger, _ := b.requestContext(r)
	logger.Error("failed to handle request",
		zap.Int("status", status),
		zap.Duration("took", time.Since(r.Context().Value(StartTimeKey{}).(time.Time))),
		zap.Error(err),
	)
	http.Error(w, err.Error(), status)
}

func (b *BeaconProxy) respond(r *http.Request, w http.ResponseWriter, data any) error {
	var response = struct {
		Data any `json:"data"`
	}{
		Data: data,
	}
	return json.NewEncoder(w).Encode(response)
}

func parseIndicesFromRequest(
	r *http.Request,
	requireIndices bool,
) ([]phase0.ValidatorIndex, error) {
	var indices []string
	if err := json.NewDecoder(r.Body).Decode(&indices); err != nil {
		// EOF is returned when there is no body (such as in GET requests).
		if errors.Is(err, io.EOF) {
			if requireIndices {
				return nil, fmt.Errorf("no indices provided")
			}
		} else {
			return nil, fmt.Errorf("failed to parse indices: %w", err)
		}
	}
	indicesOut := make([]phase0.ValidatorIndex, len(indices))
	for i, indexStr := range indices {
		n, err := strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse index %s: %w", indexStr, err)
		}
		indicesOut[i] = phase0.ValidatorIndex(n)
	}
	return indicesOut, nil
}

func scanURL(r *http.Request, fields ...any) error {
	if len(fields)%2 != 0 {
		return fmt.Errorf("invalid number of arguments to scanURL")
	}
	for i := 0; i < len(fields); i += 2 {
		fieldNameAndFormat := strings.Split(fields[i].(string), ":")
		fieldName := fieldNameAndFormat[0]
		if fieldName == "" {
			return fmt.Errorf("field name must be a string")
		}
		format := fieldNameAndFormat[1]
		if format == "" {
			return fmt.Errorf("format must be a string")
		}
		valueStr := r.URL.Query().Get(fieldName)
		if valueStr == "" {
			return nil
		}
		if format == "%x" {
			valueStr = strings.TrimPrefix(valueStr, "0x")
		}
		_, err := fmt.Sscanf(valueStr, format, fields[i+1])
		if err != nil {
			return fmt.Errorf("failed to scan field %s: %w", fieldName, err)
		}
	}

	return nil
}
