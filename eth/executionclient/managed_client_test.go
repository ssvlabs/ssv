package executionclient

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewManagedClient_Success verifies that ManagedClient successfully connects.
func TestNewManagedClient_Success(t *testing.T) {
	logger, _ := getTestLogger(t)
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	initialInterval := 500 * time.Millisecond
	maxInterval := 2 * time.Second
	syncDistanceTolerance := uint64(10)

	mc, err := NewManagedClient(context.Background(), addr, logger, initialInterval, maxInterval, syncDistanceTolerance)
	require.NoError(t, err)
	require.NotNil(t, mc)

	// Allow some time for the health monitor to perform its first check
	time.Sleep(100 * time.Millisecond)

	assert.True(t, mc.isHealthy(), "ManagedClient should be healthy after successful connection")

	err = mc.Close()
	require.NoError(t, err)
}

// TestNewManagedClient_Failure verifies that ManagedClient fails to connect with invalid address.
func TestNewManagedClient_Failure(t *testing.T) {
	logger, _ := getTestLogger(t)
	invalidAddr := "ws://localhost:12345" // Assuming nothing is running here

	initialInterval := 500 * time.Millisecond
	maxInterval := 2 * time.Second
	syncDistanceTolerance := uint64(10)

	mc, err := NewManagedClient(context.Background(), invalidAddr, logger, initialInterval, maxInterval, syncDistanceTolerance)
	require.Error(t, err)
	require.Nil(t, mc)
}

// TestManagedClient_Close verifies that ManagedClient gracefully shuts down.
func TestManagedClient_Close(t *testing.T) {
	logger, _ := getTestLogger(t)
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	addr := httpToWebSocketURL(httpsrv.URL)

	initialInterval := 500 * time.Millisecond
	maxInterval := 2 * time.Second
	syncDistanceTolerance := uint64(10)

	mc, err := NewManagedClient(context.Background(), addr, logger, initialInterval, maxInterval, syncDistanceTolerance)
	require.NoError(t, err)
	require.NotNil(t, mc)

	// Allow some time for initial connection and health check
	time.Sleep(100 * time.Millisecond)

	assert.True(t, mc.isHealthy(), "ManagedClient should be healthy after successful connection")

	// Close the ManagedClient
	err = mc.Close()
	require.NoError(t, err)

	// Verify that it's no longer healthy
	assert.False(t, mc.isHealthy(), "ManagedClient should be unhealthy after Close")

	// Attempt to perform a health check to ensure no further reconnections are attempted
	time.Sleep(300 * time.Millisecond) // Wait beyond any possible reconnection attempts

	// No change should occur; `ManagedClient` remains unhealthy
	assert.False(t, mc.isHealthy(), "ManagedClient should remain unhealthy after Close")
}

// waitForCondition waits until the condition function returns true or the timeout is reached.
func waitForCondition(timeout time.Duration, condition func() bool) bool {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			if condition() {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}
