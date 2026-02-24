package builderendpoint

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidfetcher"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidprovider"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bidstrategy"
	"github.com/ssvlabs/ssv/mev/builderendpoint/config"
	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	"github.com/ssvlabs/ssv/mev/builderendpoint/registrations"
	"github.com/ssvlabs/ssv/mev/builderendpoint/relayclient"
	"github.com/ssvlabs/ssv/mev/builderendpoint/unblinder"
)

// Server is the application-layer entrypoint for the SSV-hosted Builder API endpoint.
// Transport details live in `httpapi`, and config lives in `config`.
type Server struct {
	logger     *zap.Logger
	httpServer *http.Server

	// Exposed for internal wiring in later steps (prefetch triggers).
	cache    *bidcache.Cache
	prefetch *bidcache.Prefetcher
	fetcher  bidcache.Fetcher
}

type Dependencies struct {
	SlotStartTime func(phase0.Slot) time.Time
}

func New(ctx context.Context, logger *zap.Logger, cfg config.Config, deps Dependencies) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cache := bidcache.New(cfg.CacheTTL)
	factory := relayclient.NewFactory(ctx, cfg.RelayRequestTimeout)

	fetcher := &bidfetcher.RelayFetcher{
		Factory:       factory,
		Relays:        cfg.Relays,
		SlotStartTime: deps.SlotStartTime,
		Strategy: bidstrategy.DeadlineStrategy{
			Deadline: cfg.BidDeadline,
			BidGap:   cfg.BidGap,
		},
	}

	prefetcher := bidcache.NewPrefetcher(cache, fetcher, cfg.PrefetchMaxInFlight)

	var bidProv domain.BidProvider = &bidprovider.FetchingCached{
		Cache:   cache,
		Fetcher: fetcher,
	}

	unblind := buildUnblinder(cache, factory, cfg)

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      logger,
		BidProvider: bidProv,
		Unblinder:   unblind,
		Registrar:   buildRegistrar(factory, cfg),
	})

	return &Server{
		logger:   logger,
		cache:    cache,
		prefetch: prefetcher,
		fetcher:  fetcher,
		httpServer: &http.Server{
			Addr:              cfg.ListenAddress,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		},
	}, nil
}

func buildUnblinder(cache *bidcache.Cache, factory *relayclient.Factory, cfg config.Config) domain.Unblinder {
	if factory == nil || len(cfg.Relays) == 0 {
		return domain.NoopUnblinder{}
	}
	providers := make([]unblinder.UnblindProvider, 0, len(cfg.Relays))
	for _, relay := range cfg.Relays {
		p, err := factory.FetchUnblindProvider(relay)
		if err != nil {
			continue
		}
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return domain.NoopUnblinder{}
	}
	return &unblinder.ProvenanceRoutingUnblinder{
		Cache:            cache,
		Providers:        providers,
		PrimaryHeadStart: cfg.UnblindProvenanceHeadStart,
		Retries:          cfg.UnblindRetries,
		RetryInterval:    cfg.UnblindRetryInterval,
	}
}

func buildRegistrar(factory *relayclient.Factory, cfg config.Config) domain.RegistrationForwarder {
	if factory == nil || len(cfg.Relays) == 0 {
		return domain.NoopRegistrationForwarder{}
	}
	return &registrations.Forwarder{
		Factory: factory,
		Relays:  cfg.Relays,
	}
}

// Run serves until ctx is canceled or the underlying server returns an error.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if s.logger != nil {
			s.logger.Info("serving builder endpoint", zap.String("addr", s.httpServer.Addr))
		}
		errCh <- s.httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// PrefetchBid warms the bid cache for the given (slot, parentHash, pubkey) key.
//
// This is intended to be called from the node's duty pipeline so that when the local
// beacon client calls the Builder API's getHeader endpoint, the response is already
// cached and fast.
func (s *Server) PrefetchBid(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) {
	if s == nil || s.prefetch == nil {
		return
	}

	key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}

	// Skip duplicate work if the cache is already warm.
	if s.cache != nil {
		if _, ok := s.cache.Get(key); ok {
			return
		}
	}

	s.prefetch.Prefetch(ctx, key)
}

// PrefetchBidSync fetches and caches the best bid for the given key before returning.
//
// This is primarily intended for integration harnesses and one-shot prewarming.
// The duty pipeline should use PrefetchBid() to avoid blocking.
func (s *Server) PrefetchBidSync(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) error {
	if s == nil || s.cache == nil || s.fetcher == nil {
		return nil
	}

	key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}
	if _, ok := s.cache.Get(key); ok {
		return nil
	}

	bid, provenance, err := s.fetcher.FetchBestBid(ctx, key)
	if err != nil {
		return err
	}
	if bid == nil {
		return nil
	}
	s.cache.Put(key, bid, provenance)
	return nil
}
