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
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	Name string
	Port int
}

type BeaconProxy struct {
	remote   *url.URL
	client   eth2client.Service
	proxy    *httputil.ReverseProxy
	logger   *zap.Logger
	gateways map[int]Gateway
}

func New(ctx context.Context, logger *zap.Logger, remoteAddr string, gateways []Gateway) (*BeaconProxy, error) {
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
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
	r.HandleFunc("/", b.passthrough)
	r.HandleFunc("/eth/v1/validator/duties/attester/{epoch}", b.handleAttesterDuties)
	r.HandleFunc("/eth/v1/validator/duties/proposer/{epoch}", b.handleProposerDuties)
	r.HandleFunc("/eth/v1/validator/attestation_data", b.handleAttestationData)
	r.HandleFunc("/eth/v3/validator/blocks", b.handleBlockProposal)

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

func (b *BeaconProxy) passthrough(w http.ResponseWriter, r *http.Request) {
	r.Host = b.remote.Host
	b.proxy.ServeHTTP(w, r)
}

func (b *BeaconProxy) handleAttesterDuties(w http.ResponseWriter, r *http.Request) {
	// Parse request.
	epoch, indices, err := parseDutiesRequest(r)
	if err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to read request: %w", err))
		return
	}

	// Obtain duties.
	duties, err := b.client.(eth2client.AttesterDutiesProvider).AttesterDuties(r.Context(), epoch, indices)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attester duties: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, duties); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	b.logger.Info("obtained attester duties",
		zap.Uint64("epoch", uint64(epoch)),
		zap.Int("indices", len(indices)),
	)
}

func (b *BeaconProxy) handleProposerDuties(w http.ResponseWriter, r *http.Request) {
	// Parse request.
	epoch, indices, err := parseDutiesRequest(r)
	if err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to read request: %w", err))
		return
	}

	// Obtain duties.
	duties, err := b.client.(eth2client.ProposerDutiesProvider).ProposerDuties(r.Context(), epoch, indices)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain proposer duties: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, duties); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	b.logger.Info("obtained proposer duties",
		zap.Uint64("epoch", uint64(epoch)),
		zap.Int("indices", len(indices)),
	)
}

func (b *BeaconProxy) handleAttestationData(w http.ResponseWriter, r *http.Request) {
	// Parse request.
	var (
		slot           phase0.Slot
		committeeIndex phase0.CommitteeIndex
	)
	if err := scanURL(r, "slot:%d", &slot, "committee_index:%d", &committeeIndex); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Obtain attestation data.
	attestationData, err := b.client.(eth2client.AttestationDataProvider).AttestationData(r.Context(), slot, committeeIndex)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attestation data: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, attestationData); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	b.logger.Info("obtained attestation data",
		zap.Uint64("slot", uint64(slot)),
		zap.Uint64("committee_index", uint64(committeeIndex)),
	)
}

func (b *BeaconProxy) handleBlockProposal(w http.ResponseWriter, r *http.Request) {
	// Parse request.
	var (
		slot         phase0.Slot
		randaoReveal []byte
		graffiti     []byte
	)
	if err := scanURL(r, "slot:%d", &slot, "randao_reveal:%x", &randaoReveal, "graffiti:%x", &graffiti); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Obtain block.
	versionedBlock, err := b.client.(eth2client.BeaconBlockProposalProvider).BeaconBlockProposal(r.Context(), slot, phase0.BLSSignature(randaoReveal), graffiti)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain block: %w", err))
		return
	}
	var block any
	switch versionedBlock.Version {
	case spec.DataVersionCapella:
		block = versionedBlock.Capella
	default:
		b.error(r, w, 500, fmt.Errorf("unsupported block version %d", versionedBlock.Version))
		return
	}

	// Respond.
	var response = struct {
		Version spec.DataVersion `json:"version"`
		Data    any              `json:"data"`
	}{
		Version: versionedBlock.Version,
		Data:    block,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}
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
