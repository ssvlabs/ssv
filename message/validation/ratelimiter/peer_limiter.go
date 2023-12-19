package ratelimiter

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

type Config struct {
	// InvalidRSAs is the number of invalid RSA messages allowed per second
	InvalidRSAs int

	// InvalidMessages is the number of invalid messages allowed per second
	InvalidMessages int

	// BlockDuration is the duration for which a peer is blocked after exceeding the rate limit
	BlockDuration time.Duration

	// CacheSize is the number of peers to keep in the cache
	CacheSize int
}

func DefaultConfig() Config {
	return Config{
		InvalidRSAs:     20,
		InvalidMessages: 70000,
		BlockDuration:   1 * time.Minute,
		CacheSize:       100,
	}
}

type RateLimiter struct {
	limiters      *lru.Cache
	rsaRate       rate.Limit
	messageRate   rate.Limit
	blockDuration time.Duration
}

func New(config Config) *RateLimiter {
	cache, _ := lru.New(config.CacheSize)
	return &RateLimiter{
		limiters:      cache,
		rsaRate:       rate.Limit(config.InvalidRSAs),
		messageRate:   rate.Limit(config.InvalidMessages),
		blockDuration: config.BlockDuration,
	}
}

func (l *RateLimiter) AllowRequest(peerID peer.ID) bool {
	limiter := l.peerLimiter(peerID, false)
	if limiter == nil {
		return true
	}
	return limiter.AllowRequest(l.blockDuration)
}

func (l *RateLimiter) RegisterInvalidMessage(peerID peer.ID) {
	limiter := l.peerLimiter(peerID, true)
	limiter.RegisterInvalidMessage()
}

func (l *RateLimiter) RegisterInvalidRSA(peerID peer.ID) {
	limiter := l.peerLimiter(peerID, true)
	limiter.RegisterInvalidRSA()
}

func (l *RateLimiter) peerLimiter(peerID peer.ID, createIfMissing bool) *peerLimiter {
	if limiter, ok := l.limiters.Get(peerID); ok {
		return limiter.(*peerLimiter)
	}
	if createIfMissing {
		limiter := newPeerLimiter(l.rsaRate, l.messageRate)
		l.limiters.Add(peerID, limiter)
		return limiter
	}
	return nil
}

type peerLimiter struct {
	rsaLimiter     *rate.Limiter
	messageLimiter *rate.Limiter
	blockRequests  bool
	lastBlocked    time.Time
	mu             sync.Mutex // Mutex to protect blockRequests and lastBlocked
}

func newPeerLimiter(rsaLimit, invalidLimit rate.Limit) *peerLimiter {
	return &peerLimiter{
		rsaLimiter:     rate.NewLimiter(rsaLimit, int(rsaLimit)),
		messageLimiter: rate.NewLimiter(invalidLimit, int(invalidLimit)),
	}
}

func (rl *peerLimiter) AllowRequest(blockingTime time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.blockRequests {
		if time.Since(rl.lastBlocked) > blockingTime {
			rl.blockRequests = false
		} else {
			return false
		}
	}

	if rl.rsaLimiter.Tokens() < 1.0 || rl.messageLimiter.Tokens() < 1.0 {
		rl.blockRequests = true
		rl.lastBlocked = time.Now()
		return false
	}

	return true
}

func (rl *peerLimiter) RegisterInvalidRSA() {
	rl.rsaLimiter.Allow()
}

func (rl *peerLimiter) RegisterInvalidMessage() {
	rl.messageLimiter.Allow()
}
