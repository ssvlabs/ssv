package validation

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func TestLimiterRateLimiting(t *testing.T) {
	pl := NewPeerRateLimiter(1*time.Second, 1) // 1 request per second
	peerID := peer.ID("test-peer-3")

	assert.True(t, pl.CanProceed(peerID), "CanProceed should allow the first request")
	pl.Increment(peerID)
	assert.False(t, pl.CanProceed(peerID), "CanProceed should block the second immediate request due to rate limiting")

	time.Sleep(1 * time.Second) // Wait for the rate limiter to reset
	assert.True(t, pl.CanProceed(peerID), "CanProceed should allow the request after waiting for the rate limiter to reset")
}

// TestIncrementConcurrent tests the Increment function in a concurrent environment
func TestIncrementConcurrent(t *testing.T) {
	pl := NewPeerRateLimiter(1*time.Second, 10)
	var wg sync.WaitGroup
	peerID := peer.ID("test-peer-increment")

	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pl.Increment(peerID)
		}()
	}

	wg.Wait()
	// After concurrent increments, the limiter should block new requests
	assert.False(t, pl.CanProceed(peerID), "Should block after multiple concurrent increments")
	time.Sleep(200 * time.Millisecond) // Wait for the rate limiter to reset
	assert.False(t, pl.CanProceed(peerID), "Should block after multiple concurrent increments")
	time.Sleep(200 * time.Millisecond) // Wait for the rate limiter to reset
	assert.False(t, pl.CanProceed(peerID), "Should block after multiple concurrent increments")
	time.Sleep(700 * time.Millisecond) // Wait for the rate limiter to reset
	assert.True(t, pl.CanProceed(peerID), "Should alow after multiple concurrent increments")
}
