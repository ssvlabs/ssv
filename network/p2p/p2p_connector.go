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

type connCacheData struct {
	nextTry time.Time
	start   libp2pdiscbackoff.BackoffStrategy
}

// backoffConnector connects to proposed peers, skipping peers that are still within their backoff
// period. It is a drop-in replacement for libp2p's discovery/backoff.BackoffConnector with one
// difference: dial failures are logged to the node's logger. The libp2p implementation reports
// them only to its own internal logger, so a node that cannot reach its proposed peers (firewall,
// NAT, wrong advertised address) leaves no trace of that in SSV logs.
type backoffConnector struct {
	logger *zap.Logger

	host       host.Host
	connTryDur time.Duration
	backoff    libp2pdiscbackoff.BackoffFactory

	cacheMux sync.Mutex
	cache    *lru.TwoQueueCache[peer.ID, *connCacheData]
}

func newBackoffConnector(
	logger *zap.Logger,
	host host.Host,
	cacheSize int,
	connTryDur time.Duration,
	backoff libp2pdiscbackoff.BackoffFactory,
) (*backoffConnector, error) {
	cache, err := lru.New2Q[peer.ID, *connCacheData](cacheSize)
	if err != nil {
		return nil, err
	}

	return &backoffConnector{
		logger:     logger,
		host:       host,
		connTryDur: connTryDur,
		backoff:    backoff,
		cache:      cache,
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

			c.cacheMux.Lock()
			if cached, ok := c.cache.Get(pi.ID); ok {
				now := time.Now()
				if now.Before(cached.nextTry) {
					c.cacheMux.Unlock()
					continue
				}
				cached.nextTry = now.Add(cached.start.Delay())
			} else {
				cached = &connCacheData{start: c.backoff()}
				cached.nextTry = time.Now().Add(cached.start.Delay())
				c.cache.Add(pi.ID, cached)
			}
			c.cacheMux.Unlock()

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
