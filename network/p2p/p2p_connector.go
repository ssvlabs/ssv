package p2pv1

import (
	"context"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// backoffConnector connects to proposed peers, skipping peers that are still within their backoff
// period. It is a drop-in replacement for libp2p's discovery/backoff.BackoffConnector with one
// difference: dial failures are logged to the node's logger. The libp2p implementation reports
// them only to its own internal logger, so a node that cannot reach its proposed peers (firewall,
// NAT, wrong advertised address) leaves no trace of that in SSV logs.
type backoffConnector struct {
	host       host.Host
	logger     *zap.Logger
	cache      *lru.TwoQueueCache[peer.ID, *connCacheData]
	connTryDur time.Duration
	backoff    libp2pdiscbackoff.BackoffFactory
	mux        sync.Mutex
}

type connCacheData struct {
	nextTry time.Time
	strat   libp2pdiscbackoff.BackoffStrategy
}

func newBackoffConnector(
	host host.Host,
	logger *zap.Logger,
	cacheSize int,
	connTryDur time.Duration,
	backoff libp2pdiscbackoff.BackoffFactory,
) (*backoffConnector, error) {
	cache, err := lru.New2Q[peer.ID, *connCacheData](cacheSize)
	if err != nil {
		return nil, err
	}

	return &backoffConnector{
		host:       host,
		logger:     logger,
		cache:      cache,
		connTryDur: connTryDur,
		backoff:    backoff,
	}, nil
}

// Connect attempts to connect to the peers passed in by peerCh. Will not connect to peers if they
// are within the backoff period.
func (c *backoffConnector) Connect(ctx context.Context, peerCh <-chan peer.AddrInfo) {
	for {
		select {
		case pi, ok := <-peerCh:
			if !ok {
				return
			}

			if pi.ID == c.host.ID() || pi.ID == "" {
				continue
			}

			c.mux.Lock()
			if cached, ok := c.cache.Get(pi.ID); ok {
				now := time.Now()
				if now.Before(cached.nextTry) {
					c.mux.Unlock()
					continue
				}
				cached.nextTry = now.Add(cached.strat.Delay())
			} else {
				cached = &connCacheData{strat: c.backoff()}
				cached.nextTry = time.Now().Add(cached.strat.Delay())
				c.cache.Add(pi.ID, cached)
			}
			c.mux.Unlock()

			go func(pi peer.AddrInfo) {
				ctx, cancel := context.WithTimeout(ctx, c.connTryDur)
				defer cancel()

				if err := c.host.Connect(ctx, pi); err != nil {
					c.logger.Debug("could not connect to discovered peer",
						fields.PeerID(pi.ID),
						zap.Stringers("addrs", pi.Addrs),
						zap.Error(err))
				}
			}(pi)

		case <-ctx.Done():
			return
		}
	}
}
