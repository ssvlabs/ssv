package validation

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

type RateLimiter struct {
	rejectLimiter *rate.Limiter
	ignoreLimiter *rate.Limiter
	blockRequests bool
	lastBlocked   time.Time
	mu            sync.Mutex // Mutex to protect blockRequests and lastBlocked
}

func NewRateLimiter(rejectLimit, ignoreLimit rate.Limit) *RateLimiter {
	return &RateLimiter{
		rejectLimiter: rate.NewLimiter(rejectLimit, int(rejectLimit)),
		ignoreLimiter: rate.NewLimiter(ignoreLimit, int(ignoreLimit)),
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

	if rl.rejectLimiter.Tokens() < 1.0 || rl.ignoreLimiter.Tokens() < 1.0 {
		rl.blockRequests = true
		rl.lastBlocked = time.Now()
		return false
	}

	return true
}

func (rl *RateLimiter) RegisterRequest(isReject bool) {
	if isReject {
		rl.rejectLimiter.Allow() // Consume a token from the reject limiter
	} else {
		rl.ignoreLimiter.Allow() // Consume a token from the ignore limiter
	}
}

type PeerRateLimitManager struct {
	limiters     *lru.Cache
	rejectRate   rate.Limit
	ignoreRate   rate.Limit
	blockingTime time.Duration
}

func NewPeerRateLimitManager(rejectRate, ignoreRate, cacheSize int, blockingTime time.Duration) *PeerRateLimitManager {
	cache, _ := lru.New(cacheSize)
	return &PeerRateLimitManager{
		limiters:     cache,
		rejectRate:   rate.Limit(rejectRate),
		ignoreRate:   rate.Limit(ignoreRate),
		blockingTime: blockingTime,
	}
}

func (p *PeerRateLimitManager) GetLimiter(peerID peer.ID, createIfMissing bool) *RateLimiter {
	if limiter, ok := p.limiters.Get(peerID); ok {
		return limiter.(*RateLimiter)
	}
	if createIfMissing {
		limiter := NewRateLimiter(p.rejectRate, p.ignoreRate)
		p.limiters.Add(peerID, limiter)
		return limiter
	}
	return nil
}

func (p *PeerRateLimitManager) AllowRequest(peerID peer.ID) bool {
	limiter := p.GetLimiter(peerID, false)
	if limiter == nil {
		return true
	}
	return limiter.AllowRequest(p.blockingTime)
}

func (p *PeerRateLimitManager) RegisterIgnoreRequest(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.RegisterRequest(false)
}

func (p *PeerRateLimitManager) RegisterRejectRequest(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.RegisterRequest(true)
}
