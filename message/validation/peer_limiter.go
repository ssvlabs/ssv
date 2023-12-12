package validation

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

type RateLimiter struct {
	rsaLimiter     *rate.Limiter
	invalidLimiter *rate.Limiter
	blockRequests  bool
	lastBlocked    time.Time
	mu             sync.Mutex // Mutex to protect blockRequests and lastBlocked
}

func NewRateLimiter(rsaLimit, invalidLimit rate.Limit) *RateLimiter {
	return &RateLimiter{
		rsaLimiter:     rate.NewLimiter(rsaLimit, int(rsaLimit)),
		invalidLimiter: rate.NewLimiter(invalidLimit, int(invalidLimit)),
	}
}

func (rl *RateLimiter) AllowRequest(blockingTime time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.blockRequests {
		if time.Since(rl.lastBlocked) > blockingTime {
			rl.blockRequests = false
		} else {
			return false
		}
	}

	if rl.rsaLimiter.Tokens() < 1.0 || rl.invalidLimiter.Tokens() < 1.0 {
		rl.blockRequests = true
		rl.lastBlocked = time.Now()
		return false
	}

	return true
}

func (rl *RateLimiter) RegisterRSAError() {
	rl.rsaLimiter.Allow()
}

func (rl *RateLimiter) RegisterInvalid() {
	rl.invalidLimiter.Allow()
}

type PeerRateLimiter struct {
	limiters     *lru.Cache
	rsaRate      rate.Limit
	invalidRate  rate.Limit
	blockingTime time.Duration
}

func NewPeerRateLimiter(rsaRate, invalidRate, cacheSize int, blockingTime time.Duration) *PeerRateLimiter {
	cache, _ := lru.New(cacheSize)
	return &PeerRateLimiter{
		limiters:     cache,
		rsaRate:      rate.Limit(rsaRate),
		invalidRate:  rate.Limit(invalidRate),
		blockingTime: blockingTime,
	}
}

func (p *PeerRateLimiter) GetLimiter(peerID peer.ID, createIfMissing bool) *RateLimiter {
	if limiter, ok := p.limiters.Get(peerID); ok {
		return limiter.(*RateLimiter)
	}
	if createIfMissing {
		limiter := NewRateLimiter(p.rsaRate, p.invalidRate)
		p.limiters.Add(peerID, limiter)
		return limiter
	}
	return nil
}

func (p *PeerRateLimiter) AllowRequest(peerID peer.ID) bool {
	limiter := p.GetLimiter(peerID, false)
	if limiter == nil {
		return true
	}
	return limiter.AllowRequest(p.blockingTime)
}

func (p *PeerRateLimiter) RegisterInvalidRequest(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.RegisterInvalid()
}

func (p *PeerRateLimiter) RegisterRSAErrorRequest(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.RegisterRSAError()
}
