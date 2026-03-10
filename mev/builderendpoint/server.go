package builderendpoint

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/bids"
	"github.com/ssvlabs/ssv/mev/builderendpoint/config"
	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	"github.com/ssvlabs/ssv/mev/builderendpoint/relayclient"
	"github.com/ssvlabs/ssv/mev/builderendpoint/stats"
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
	stats    *stats.Collector

	slotStartTime func(phase0.Slot) time.Time

	cacheCleanupInterval time.Duration
	relaysCount          int
}

type Dependencies struct {
	SlotStartTime func(phase0.Slot) time.Time
}

func New(ctx context.Context, logger *zap.Logger, cfg config.Config, deps Dependencies) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("MevBuilder")

	cache := bidcache.New(cfg.CacheTTL)
	factory := relayclient.NewFactory(ctx, cfg.RelayRequestTimeout)
	collector := stats.NewCollector(stats.Options{})

	baseFetcher := &bids.RelayFetcher{
		Factory:       factory,
		Relays:        cfg.Relays,
		SlotStartTime: deps.SlotStartTime,
		Strategy: bids.DeadlineStrategy{
			Deadline: cfg.BidDeadline,
			BidGap:   cfg.BidGap,
		},
	}

	fetcherForHeader := &bids.FetcherWithMetrics{Source: "get_header", Next: baseFetcher, Observer: collector}
	fetcherForPrefetch := &bids.FetcherWithMetrics{Source: "prefetch", Next: baseFetcher, Observer: collector}

	prefetcher := bidcache.NewPrefetcher(cache, fetcherForPrefetch, cfg.PrefetchMaxInFlight, bidcache.WithPrefetchObserver(collector))

	var bidSF singleflight.Group
	bidProvider := func(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
		if ctx == nil {
			ctx = context.Background()
		}

		slotStart := time.Time{}
		if deps.SlotStartTime != nil {
			slotStart = deps.SlotStartTime(slot)
		}
		slotOffset := time.Duration(0)
		if !slotStart.IsZero() {
			slotOffset = time.Since(slotStart)
		}

		start := time.Now()
		key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}

		cacheRes := getHeaderCacheMiss
		result := getHeaderResultError
		defer func() {
			took := time.Since(start)
			recordGetHeaderSlotOffset(ctx, cacheRes, result, slotOffset)
			recordGetHeader(ctx, cacheRes, result, took)
			if collector != nil {
				collector.ObserveGetHeader(string(cacheRes), string(result), took, slotOffset)
			}
		}()

		if cache != nil {
			if ent, ok := cache.Get(key); ok {
				cacheRes = getHeaderCacheHit
				result = getHeaderResultBid
				return ent.Bid, nil
			}
		}

		bid, err := bids.GetBidSingleflight(ctx, cache, fetcherForHeader, &bidSF, key)
		if err != nil {
			result = getHeaderResultError
			return nil, err
		}
		if bid == nil {
			result = getHeaderResultNoBid
			return nil, nil
		}
		result = getHeaderResultBid
		return bid, nil
	}

	unblind := buildUnblinder(cache, factory, cfg)

	handler := httpapi.NewRouter(logger.Named("HTTP"), bidProvider, unblind, buildRegistrar(factory, cfg))

	cleanupInterval := cfg.CacheCleanupInterval
	if cfg.CacheTTL <= 0 {
		// No TTL eviction configured, so periodic cleanup would be a no-op.
		cleanupInterval = 0
	}

	return &Server{
		logger:               logger,
		cache:                cache,
		prefetch:             prefetcher,
		fetcher:              fetcherForPrefetch,
		stats:                collector,
		slotStartTime:        deps.SlotStartTime,
		cacheCleanupInterval: cleanupInterval,
		relaysCount:          len(cfg.Relays),
		httpServer: &http.Server{
			Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		},
	}, nil
}

func buildUnblinder(cache *bidcache.Cache, factory *relayclient.Factory, cfg config.Config) httpapi.UnblinderFunc {
	if factory == nil || len(cfg.Relays) == 0 {
		return nil
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
		return nil
	}
	u := &unblinder.ProvenanceRoutingUnblinder{
		Cache:            cache,
		Providers:        providers,
		PrimaryHeadStart: cfg.UnblindProvenanceHeadStart,
		Retries:          cfg.UnblindRetries,
		RetryInterval:    cfg.UnblindRetryInterval,
	}
	return u.UnblindBlock
}

func buildRegistrar(factory *relayclient.Factory, cfg config.Config) httpapi.ValidatorRegistrationsForwarderFunc {
	if factory == nil || len(cfg.Relays) == 0 {
		return nil
	}
	fwd := &RegistrationsForwarder{
		Factory: factory,
		Relays:  cfg.Relays,
	}
	return fwd.ForwardValidatorRegistrations
}

// Run serves until ctx is canceled or the underlying server returns an error.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	if s.cache != nil && s.cacheCleanupInterval > 0 {
		go s.runCacheJanitor(ctx, s.cacheCleanupInterval)
	}
	if s.stats != nil {
		go s.runHourlyStatsReporter(ctx, time.Hour)
	}

	go func() {
		if s.logger != nil {
			s.logger.Info(
				"serving builder endpoint",
				zap.String("addr", s.httpServer.Addr),
				zap.Int("relays", s.relaysCount),
			)
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

func (s *Server) runCacheJanitor(ctx context.Context, interval time.Duration) {
	if s == nil || s.cache == nil || interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cache.CleanupExpired()
			bidEntries, provenanceEntries := s.cache.Sizes()
			inFlight := 0
			if s.prefetch != nil {
				inFlight = s.prefetch.InFlight()
			}
			recordCacheGauges(ctx, bidEntries, provenanceEntries, inFlight)
		}
	}
}

func (s *Server) runHourlyStatsReporter(ctx context.Context, interval time.Duration) {
	if s == nil || s.stats == nil || s.logger == nil || interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger := s.logger.Named("Stats")

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			report := s.stats.SnapshotAndReset(t)
			logger.Info("mev builder hourly report", zap.Any("report", report))
		}
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

	if s.slotStartTime != nil {
		lead := time.Until(s.slotStartTime(slot))
		recordPrefetchLeadTime(ctx, lead)
		if s.stats != nil {
			s.stats.ObservePrefetchLeadTime(lead)
		}
	}

	key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}

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

	if s.slotStartTime != nil {
		lead := time.Until(s.slotStartTime(slot))
		recordPrefetchLeadTime(ctx, lead)
		if s.stats != nil {
			s.stats.ObservePrefetchLeadTime(lead)
		}
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
