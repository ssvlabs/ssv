package validation

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func TestLimiterBasicRateLimiting(t *testing.T) {
	pl := NewPeerRateLimitManager(1, 1*time.Second) // 1 request per second
	peerID := peer.ID("test-peer-1")

	assert.True(t, pl.AllowRequest(peerID), "CanProceed should allow the first request")
	pl.RegisterRequest(peerID)

	// Immediate subsequent request should be blocked
	assert.False(t, pl.AllowRequest(peerID), "CanProceed should block the second immediate request due to rate limiting")

	time.Sleep(1 * time.Second) // Wait for the rate limiter to reset
	assert.True(t, pl.AllowRequest(peerID), "CanProceed should allow the request after waiting for the rate limiter to reset")
}

// TestIncrementConcurrent tests the Increment function in a concurrent environment
func TestIncrementConcurrent(t *testing.T) {
	pl := NewPeerRateLimitManager(10, 1*time.Second) // 10 requests per second
	peerID := peer.ID("test-peer-2")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pl.RegisterRequest(peerID)
		}()
	}

	wg.Wait()

	// After concurrent increments, the limiter should block new requests if exceeded
	assert.False(t, pl.AllowRequest(peerID), "Should block after multiple concurrent increments")
}

func TestBlockingBehavior(t *testing.T) {
	pl := NewPeerRateLimitManager(5, 1*time.Second) // 5 requests per second
	peerID := peer.ID("test-peer-3")

	for i := 0; i < 5; i++ {
		assert.True(t, pl.AllowRequest(peerID), "Iteration %d: Should allow within rate limit", i)
		pl.RegisterRequest(peerID)
	}

	// Next request should be blocked
	assert.False(t, pl.AllowRequest(peerID), "Should block after exceeding rate limit")

	// Wait for the limiter to reset
	time.Sleep(1 * time.Second)
	assert.True(t, pl.AllowRequest(peerID), "Should allow after rate limiter resets")
}
