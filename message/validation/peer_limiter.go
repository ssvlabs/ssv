package validation

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

// Define a structure for PeerRateLimiter
type PeerRateLimiter struct {
	limiters map[peer.ID]*rate.Limiter
	rate     rate.Limit
	burst    int
	mu       sync.Mutex
}

// NewPeerRateLimiter creates a new instance of PeerRateLimiter
func NewPeerRateLimiter(duration time.Duration, totalCount int) *PeerRateLimiter {
	return &PeerRateLimiter{
		rate:     rate.Every(duration),
		burst:    totalCount,
		limiters: make(map[peer.ID]*rate.Limiter),
	}
}

// GetLimiter returns the rate limiter for a given peer ID
func (p *PeerRateLimiter) GetLimiter(peerID peer.ID, create bool) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if create && p.limiters[peerID] == nil {
		p.limiters[peerID] = rate.NewLimiter(p.rate, p.burst)
	}
	return p.limiters[peerID]
}

// CanProceed checks if the peer with given ID can proceed with an operation
func (p *PeerRateLimiter) CanProceed(peerID peer.ID) bool {
	limiter := p.GetLimiter(peerID, false)
	if limiter == nil {
		return true
	}
	return limiter.Allow()
}

// Increment consumes a token for the peer, indicating an operation has been performed
func (p *PeerRateLimiter) Increment(peerID peer.ID) {
	limiter := p.GetLimiter(peerID, true)
	limiter.Reserve().Cancel()
}
