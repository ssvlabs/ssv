package builderendpoint

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
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
	"github.com/ssvlabs/ssv/mev/builderendpoint/relayurl"
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
	fetcher  bidcache.Fetcher // used by prefetcher / stats
	stats    *stats.Collector

	fetcherForHeader bidcache.Fetcher
	bidSF            *singleflight.Group

	slotStartTime func(phase0.Slot) time.Time

	cacheCleanupInterval time.Duration
	relaysCount          int

	prefetchParentHashTracker *prefetchParentHashTracker

	backgroundOnce sync.Once
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

	cfg.Relays = config.NormalizeRelays(cfg.Relays)

	cache := bidcache.New(cfg.CacheTTL)
	factory := relayclient.NewFactory(ctx, cfg.RelayRequestTimeout)
	collector := stats.NewCollector(stats.Options{})

	trackerTTL := cfg.CacheTTL
	if trackerTTL < 12*time.Second {
		trackerTTL = 12 * time.Second
	}
	parentHashTracker := newPrefetchParentHashTracker(trackerTTL)

	strategy := bids.DeadlineStrategy{
		Deadline: cfg.BidDeadline,
		BidGap:   cfg.BidGap,
	}

	baseFetcherForHeader := &bids.RelayFetcher{
		Factory:       factory,
		Relays:        cfg.Relays,
		SlotStartTime: deps.SlotStartTime,
		Strategy:      strategy,
	}

	baseFetcherForPrefetch := &bids.RelayFetcher{
		Factory:       factory,
		Relays:        cfg.Relays,
		SlotStartTime: deps.SlotStartTime,
		Strategy:      strategy,
		OnBid: func(ctx context.Context, key bidcache.Key, provenance string, bid *builderspec.VersionedSignedBuilderBid) {
			first, updated := cache.PutIfBetter(key, bid, provenance)
			if !updated {
				return
			}

			if first && deps.SlotStartTime != nil {
				lead := time.Until(deps.SlotStartTime(key.Slot))
				recordPrefetchFirstCachedLeadTime(ctx, lead)
				if collector != nil {
					collector.ObservePrefetchFirstCachedLeadTime(lead)
				}
			}
		},
	}

	fetcherForHeader := &bids.FetcherWithMetrics{Source: "get_header", Next: baseFetcherForHeader, Observer: collector}
	fetcherForPrefetch := &bids.FetcherWithMetrics{Source: "prefetch", Next: baseFetcherForPrefetch, Observer: collector}

	prefetcher := bidcache.NewPrefetcher(cache, fetcherForPrefetch, cfg.PrefetchMaxInFlight, bidcache.WithPrefetchObserver(collector))

	unblind := buildUnblinder(cache, factory, cfg)

	cleanupInterval := cfg.CacheCleanupInterval
	if cfg.CacheTTL <= 0 {
		// No TTL eviction configured, so periodic cleanup would be a no-op.
		cleanupInterval = 0
	}

	srv := &Server{
		logger:                    logger,
		cache:                     cache,
		prefetch:                  prefetcher,
		fetcher:                   fetcherForPrefetch,
		stats:                     collector,
		fetcherForHeader:          fetcherForHeader,
		bidSF:                     &singleflight.Group{},
		slotStartTime:             deps.SlotStartTime,
		cacheCleanupInterval:      cleanupInterval,
		relaysCount:               len(cfg.Relays),
		prefetchParentHashTracker: parentHashTracker,
	}

	bidProvider := func(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
		bid, _, err := srv.GetHeader(ctx, "live", slot, parentHash, pubkey)
		return bid, err
	}
	handler := httpapi.NewRouter(logger.Named("HTTP"), bidProvider, unblind, buildRegistrar(factory, cfg))
	srv.httpServer = &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	return srv, nil
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

type GetHeaderReport struct {
	Cache      string        `json:"cache"`
	Result     string        `json:"result"`
	Took       time.Duration `json:"took"`
	SlotOffset time.Duration `json:"slot_offset"`

	RelayHost string  `json:"relay_host,omitempty"`
	ValueETH  float64 `json:"value_eth,omitempty"`
}

// GetHeader runs the builder getHeader flow and returns the selected bid along with a report.
//
// mode is used only for metrics labeling (e.g. "live", "dry_run").
func (s *Server) GetHeader(ctx context.Context, mode string, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, GetHeaderReport, error) {
	if s == nil || s.fetcherForHeader == nil {
		return nil, GetHeaderReport{Cache: string(getHeaderCacheMiss), Result: string(getHeaderResultError)}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	slotStart := time.Time{}
	if s.slotStartTime != nil {
		slotStart = s.slotStartTime(slot)
	}
	slotOffset := time.Duration(0)
	if !slotStart.IsZero() {
		slotOffset = time.Since(slotStart)
	}

	if s.prefetchParentHashTracker != nil {
		res := s.prefetchParentHashTracker.Compare(slot, pubkey, parentHash)
		recordPrefetchParentHashCompare(ctx, mode, res)
		if s.stats != nil {
			s.stats.ObservePrefetchParentHashCompare(string(res))
		}
	}

	start := time.Now()
	key := bidcache.Key{Slot: slot, ParentHash: parentHash, Pubkey: pubkey}

	cacheRes := getHeaderCacheMiss
	if s.cache != nil {
		if ent, ok := s.cache.Get(key); ok {
			cacheRes = getHeaderCacheHit
			took := time.Since(start)
			rep := GetHeaderReport{
				Cache:      string(cacheRes),
				Result:     string(getHeaderResultBid),
				Took:       took,
				SlotOffset: slotOffset,
			}
			if host := relayurl.Host(ent.Provenance); host != "" {
				rep.RelayHost = host
			}
			if s.stats != nil {
				s.stats.ObserveGetHeader(rep.Cache, rep.Result, rep.Took, rep.SlotOffset)
			}
			recordGetHeaderSlotOffset(ctx, mode, cacheRes, getHeaderResultBid, slotOffset)
			recordGetHeader(ctx, mode, cacheRes, getHeaderResultBid, took)
			return ent.Bid, rep, nil
		}
	}

	bid, err := bids.GetBidSingleflight(ctx, s.cache, s.fetcherForHeader, s.bidSF, key)
	if err != nil {
		took := time.Since(start)
		rep := GetHeaderReport{Cache: string(cacheRes), Result: string(getHeaderResultError), Took: took, SlotOffset: slotOffset}
		if s.stats != nil {
			s.stats.ObserveGetHeader(rep.Cache, rep.Result, rep.Took, rep.SlotOffset)
		}
		recordGetHeaderSlotOffset(ctx, mode, cacheRes, getHeaderResultError, slotOffset)
		recordGetHeader(ctx, mode, cacheRes, getHeaderResultError, took)
		return nil, rep, err
	}
	if bid == nil {
		took := time.Since(start)
		rep := GetHeaderReport{Cache: string(cacheRes), Result: string(getHeaderResultNoBid), Took: took, SlotOffset: slotOffset}
		if s.stats != nil {
			s.stats.ObserveGetHeader(rep.Cache, rep.Result, rep.Took, rep.SlotOffset)
		}
		recordGetHeaderSlotOffset(ctx, mode, cacheRes, getHeaderResultNoBid, slotOffset)
		recordGetHeader(ctx, mode, cacheRes, getHeaderResultNoBid, took)
		return nil, rep, nil
	}

	// Fetch provenance (and relay host) from cache if available.
	provenance := ""
	if s.cache != nil {
		if ent, ok := s.cache.Get(key); ok {
			provenance = ent.Provenance
		}
	}

	took := time.Since(start)
	rep := GetHeaderReport{Cache: string(cacheRes), Result: string(getHeaderResultBid), Took: took, SlotOffset: slotOffset}
	if host := relayurl.Host(provenance); host != "" {
		rep.RelayHost = host
	}
	if s.stats != nil {
		s.stats.ObserveGetHeader(rep.Cache, rep.Result, rep.Took, rep.SlotOffset)
	}
	recordGetHeaderSlotOffset(ctx, mode, cacheRes, getHeaderResultBid, slotOffset)
	recordGetHeader(ctx, mode, cacheRes, getHeaderResultBid, took)
	return bid, rep, nil
}

// StartBackground runs periodic tasks (cache cleanup and stats reporting) without starting the HTTP server.
func (s *Server) StartBackground(ctx context.Context) {
	if s == nil {
		return
	}
	s.backgroundOnce.Do(func() {
		if s.cache != nil && s.cacheCleanupInterval > 0 {
			go s.runCacheJanitor(ctx, s.cacheCleanupInterval)
		}
		if s.stats != nil {
			go s.runHourlyStatsReporter(ctx, time.Hour)
		}
	})
}

// Run serves until ctx is canceled or the underlying server returns an error.
func (s *Server) Run(ctx context.Context) error {
	s.StartBackground(ctx)

	errCh := make(chan error, 1)

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

	if s.prefetchParentHashTracker != nil {
		s.prefetchParentHashTracker.Record(slot, pubkey, parentHash)
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

	if s.prefetchParentHashTracker != nil {
		s.prefetchParentHashTracker.Record(slot, pubkey, parentHash)
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
