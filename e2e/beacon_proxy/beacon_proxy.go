package beaconproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/auto"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/e2e/beacon_proxy/intercept"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// gatewayKey is the key used to store the gateway in the request context.
var gatewayKey struct{}

// loggerKey is the key used to store the logger in the request context.
var loggerKey struct{}

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
		remote: remote,
		proxy:  httputil.NewSingleHostReverseProxy(remote),
		client: client,
		logger: logger,
	}
	for _, gateway := range gateways {
		b.gateways[gateway.Port] = gateway
	}
	return b, nil
}

func (b *BeaconProxy) Run(ctx context.Context) error {
	r := chi.NewRouter()
	r.Use(b.loggerMiddleware)
	r.HandleFunc("/", b.passthrough)

	// Attestations.
	r.HandleFunc("/eth/v1/validator/duties/attester/{epoch}", b.handleAttesterDuties)
	r.HandleFunc("/eth/v1/validator/attestation_data", b.handleAttestationData)
	r.HandleFunc("/eth/v1/beacon/pool/attestations", b.handleSubmitAttestations)

	// Proposals.
	r.HandleFunc("/eth/v1/validator/duties/proposer/{epoch}", b.handleProposerDuties)
	r.HandleFunc("/eth/v3/validator/blocks", b.handleBlockProposal)
	r.HandleFunc("/eth/v1/beacon/pool/proposals", b.handleSubmitBlockProposal)

	// Serve each endpoint on a separate port.
	pool := pool.New().WithContext(ctx)
	for port, gateway := range b.gateways {
		b.logger.Debug("starting proxy endpoint",
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
				Handler:      r,
			}
			return server.ListenAndServe()
		})
	}
	return pool.Wait()
}

func (b *BeaconProxy) loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine the gateway from the requested port.
		port, err := strconv.Atoi(r.URL.Port())
		if err != nil {
			b.logger.Fatal("failed to parse port",
				zap.String("port", r.URL.Port()),
				zap.Error(err),
			)
			return
		}
		gateway, ok := b.gateways[port]
		if !ok {
			b.logger.Fatal("unknown port", zap.String("port", r.URL.Port()))
			return
		}
		ctx := context.WithValue(r.Context(), gatewayKey, gateway)

		logger := b.logger.With(
			zap.String("gateway", gateway.Name),
			zap.String("endpoint", fmt.Sprintf("%s %s", r.Method, r.URL.Path)),
		)
		ctx = context.WithValue(ctx, loggerKey, logger)

		b.logger.Debug("received request",
			zap.String("gateway", gateway.Name),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (b *BeaconProxy) passthrough(w http.ResponseWriter, r *http.Request) {
	r.Host = b.remote.Host
	b.proxy.ServeHTTP(w, r)
}

func (b *BeaconProxy) error(r *http.Request, w http.ResponseWriter, status int, err error) {
	logger := r.Context().Value(loggerKey).(*zap.Logger)
	logger.Error("failed to handle request",
		zap.Int("status", status),
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

func parseDutiesRequest(r *http.Request) (phase0.Epoch, []phase0.ValidatorIndex, error) {
	var epoch phase0.Epoch
	if err := scanURL(r, "epoch:%d", &epoch); err != nil {
		return 0, nil, fmt.Errorf("failed to parse epoch: %w", err)
	}
	var indices []phase0.ValidatorIndex
	if err := json.NewDecoder(r.Body).Decode(&indices); err != nil {
		return 0, nil, fmt.Errorf("failed to parse indices: %w", err)
	}
	return epoch, indices, nil
}

func scanURL(r *http.Request, fields ...any) error {
	if len(fields)%2 != 0 {
		return fmt.Errorf("invalid number of arguments to scanURL")
	}
	for i := 0; i < len(fields); i += 2 {
		fieldName, ok := fields[i].(string)
		if !ok {
			return fmt.Errorf("field name must be a string")
		}
		format, ok := fields[i].(string)
		if !ok {
			return fmt.Errorf("format must be a string")
		}
		valueStr := chi.URLParam(r, fieldName)
		if valueStr == "" {
			return nil
		}
		if format == "%x" {
			valueStr = strings.TrimPrefix(valueStr, "0x")
		}
		_, err := fmt.Sscanf(valueStr, format, fields[i+1])
		if err != nil {
			return fmt.Errorf("failed to scan field %s: %v", fieldName, err)
		}
	}

	return nil
}
