package ratelimiter

import (
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func TestLimiterBasicRateLimiting(t *testing.T) {
	pl := New(Config{1, 1, 1 * time.Second, 10}) // 1 request per second
	peerID := peer.ID("test-peer-1")

	assert.True(t, pl.AllowRequest(peerID), "CanProceed should allow the first request")
	pl.RegisterInvalidMessage(peerID)

	// Immediate subsequent request should be blocked
	assert.False(t, pl.AllowRequest(peerID), "CanProceed should block the second immediate request due to rate limiting")

	time.Sleep(1 * time.Second) // Wait for the rate limiter to reset
	assert.True(t, pl.AllowRequest(peerID), "CanProceed should allow the request after waiting for the rate limiter to reset")
}

func TestMixedConcurrentRejectAndIgnoreRequests(t *testing.T) {
	pl := New(Config{5, 5, 1 * time.Second, 10})
	peerID := peer.ID("test-peer-mixed-concurrent")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				pl.RegisterInvalidMessage(peerID)
			} else {
				pl.RegisterInvalidRSA(peerID)
			}
		}(i)
	}

	wg.Wait()
	assert.False(t, pl.AllowRequest(peerID), "Should block after mixed concurrent increments")
}

func TestBlockingBehavior(t *testing.T) {
	pl := New(Config{5, 5, 1 * time.Second, 10}) // 5 requests per second
	peerID := peer.ID("test-peer-3")

	for i := 0; i < 5; i++ {
		assert.True(t, pl.AllowRequest(peerID), "Iteration %d: Should allow within rate limit", i)
		pl.RegisterInvalidMessage(peerID)
	}

	// Next request should be blocked
	assert.False(t, pl.AllowRequest(peerID), "Should block after exceeding rate limit")

	// Wait for the limiter to reset
	time.Sleep(1 * time.Second)
	assert.True(t, pl.AllowRequest(peerID), "Should allow after rate limiter resets")
}

func TestMultiplePeers(t *testing.T) {
	const rateLimit = 5
	const seconds = 2
	const blockingTime = seconds*time.Second + 50*time.Millisecond

	pl := New(Config{rateLimit, rateLimit, blockingTime, 10}) // 5 requests per second
	var wg sync.WaitGroup

	wg.Add(1)
	belowLimitPeer := peer.ID("just-below-limit")
	go func() {
		defer wg.Done()
		for i := 0; i < rateLimit*seconds+1; i++ {
			assert.True(t, pl.AllowRequest(belowLimitPeer), "Iteration %d: Should allow within rate limit", i)
			pl.RegisterInvalidRSA(belowLimitPeer)
			pl.RegisterInvalidMessage(belowLimitPeer)
			time.Sleep(time.Second / time.Duration(rateLimit))
		}
	}()

	aboveLimitPeer := peer.ID("just-above-limit")
	blocks := 0
	lastBlocked := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		blocked := false
		for i := 0; i < (rateLimit*seconds+1)*2; i++ {
			blocked = !pl.AllowRequest(aboveLimitPeer)
			if blocked {
				blocks++
				lastBlocked = time.Now()
			} else {
				pl.RegisterInvalidRSA(aboveLimitPeer)
				pl.RegisterInvalidMessage(aboveLimitPeer)
			}
			time.Sleep(time.Second / time.Duration(rateLimit) / 2)
		}
	}()

	wg.Wait()
	assert.Greater(t, blocks, 0, "Should have blocked at least once")
	assert.False(t, pl.AllowRequest(aboveLimitPeer), "Should still be blocked after exceeding rate limit")
	assert.True(t, pl.AllowRequest(belowLimitPeer), "Should allow after rate limiter resets")

	// Wait for the limiter to reset
	time.Sleep(time.Until(lastBlocked.Add(blockingTime)))
	assert.True(t, pl.AllowRequest(aboveLimitPeer), "Should allow after rate limiter resets")
}
