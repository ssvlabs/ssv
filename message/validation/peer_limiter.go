package validation

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

type RateLimiter struct {
	limiter       *rate.Limiter
	blockRequests bool
	lastBlocked   time.Time
	mu            sync.Mutex // Mutex to protect blockRequests and lastBlocked
}

func NewRateLimiter(rateLimit rate.Limit) *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rateLimit, int(rateLimit)),
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

	if rl.limiter.Tokens() < 1.0 {
		rl.blockRequests = true
		rl.lastBlocked = time.Now()
		return false
	}

	return true
}

func (rl *RateLimiter) RegisterRequest() {
	rl.limiter.Allow()
}

type PeerRateLimitManager struct {
	limiters     map[peer.ID]*RateLimiter
	rate         rate.Limit
	blockingTime time.Duration
	mu           sync.Mutex
}

func NewPeerRateLimitManager(ratePerSecond int, blockingTime time.Duration) *PeerRateLimitManager {
	return &PeerRateLimitManager{
		rate:         rate.Limit(ratePerSecond),
		blockingTime: blockingTime,
		limiters:     make(map[peer.ID]*RateLimiter),
	}
}

func (p *PeerRateLimitManager) GetLimiter(peerID peer.ID, createIfMissing bool) *RateLimiter {
	p.mu.Lock()
	defer p.mu.Unlock()

	limiter, exists := p.limiters[peerID]
	if createIfMissing && !exists {
		limiter = NewRateLimiter(p.rate)
		p.limiters[peerID] = limiter
	}
	return limiter
}

func (p *PeerRateLimitManager) AllowRequest(peerID peer.ID) bool {
	limiter := p.GetLimiter(peerID, false)
	if limiter == nil {
		return true
	}
	return limiter.AllowRequest(p.blockingTime)
}

func (p *PeerRateLimitManager) RegisterRequest(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.RegisterRequest()
}
