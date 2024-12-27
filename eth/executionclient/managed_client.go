package executionclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// ManagedClient manages an individual Ethereum client connection.
type ManagedClient struct {
	addr                        string
	client                      *ethclient.Client
	mu                          sync.RWMutex
	healthy                     bool
	logger                      *zap.Logger
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration
	syncDistanceTolerance       uint64
	quit                        chan struct{}
	wg                          sync.WaitGroup
}

// NewManagedClient creates and starts a new ManagedClient.
func NewManagedClient(ctx context.Context, addr string, logger *zap.Logger, initialInterval, maxInterval time.Duration, syncDistanceTolerance uint64) (*ManagedClient, error) {
	mc := &ManagedClient{
		addr:                        addr,
		logger:                      logger,
		reconnectionInitialInterval: initialInterval,
		reconnectionMaxInterval:     maxInterval,
		syncDistanceTolerance:       syncDistanceTolerance,
		quit:                        make(chan struct{}),
	}

	err := mc.connect(ctx)
	if err != nil {
		mc.logger.Error("Initial connection failed", zap.String("address", addr), zap.Error(err))
		return nil, fmt.Errorf("connect: %w", err)
	}

	mc.wg.Add(1)
	go mc.monitorHealth()

	return mc, nil
}

// connect establishes a connection to the Ethereum client.
func (mc *ManagedClient) connect(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	client, err := ethclient.DialContext(ctx, mc.addr)
	if err != nil {
		return err
	}

	mc.client = client
	mc.healthy = true
	mc.logger.Info("Connected to execution client", zap.String("address", mc.addr))
	return nil
}

// disconnect closes the current client connection.
func (mc *ManagedClient) disconnect() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.client != nil {
		if mc.client.Client() != nil {
			mc.client.Close()
		}
		mc.client = nil
	}
	mc.healthy = false
	mc.logger.Warn("Disconnected from execution client", zap.String("address", mc.addr))
}

// monitorHealth continuously checks the client's health and attempts reconnection if necessary.
func (mc *ManagedClient) monitorHealth() {
	defer mc.wg.Done()
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mc.quit:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), DefaultConnectionTimeout)
			err := mc.checkHealth(ctx)
			cancel()
			if err != nil {
				mc.logger.Error("Health check failed", zap.String("address", mc.addr), zap.Error(err))
				if mc.healthy {
					mc.disconnect()
				}
				mc.attemptReconnect()
			} else {
				if !mc.healthy {
					mc.logger.Info("Reconnected to execution client", zap.String("address", mc.addr))
					mc.mu.Lock()
					mc.healthy = true
					mc.mu.Unlock()
				}
			}
		}
	}
}

// checkHealth verifies if the client is responding and not syncing.
func (mc *ManagedClient) checkHealth(ctx context.Context) error {
	mc.mu.RLock()
	client := mc.client
	mc.mu.RUnlock()

	if client == nil {
		return ErrNotConnected
	}

	// Simple health check: try to get the chain ID
	sp, err := client.SyncProgress(ctx)
	if err != nil {
		return err
	}

	if sp != nil {
		syncDistance := max(sp.HighestBlock, sp.CurrentBlock) - sp.CurrentBlock

		if syncDistance > mc.syncDistanceTolerance {
			mc.logger.Warn("sync distance exceeds tolerance",
				zap.Uint64("sync_distance", syncDistance),
				zap.Uint64("tolerance", mc.syncDistanceTolerance),
				zap.String("address", mc.addr),
				zap.String("method", "eth_syncing"),
			)

			return err
		}
	}

	return nil
}

// attemptReconnect tries to reconnect with exponential backoff.
func (mc *ManagedClient) attemptReconnect() {
	backoff := mc.reconnectionInitialInterval
	for attempts := 0; attempts < maxReconnectionAttempts; attempts++ {
		select {
		case <-mc.quit:
			return
		case <-time.After(backoff):
			mc.logger.Info("Attempting to reconnect", zap.String("address", mc.addr), zap.Int("attempt", attempts+1))
			ctx, cancel := context.WithTimeout(context.Background(), DefaultConnectionTimeout)
			err := mc.connect(ctx)
			cancel()
			if err == nil {
				return
			}
			mc.logger.Error("Reconnection attempt failed", zap.String("address", mc.addr), zap.Error(err))
			backoff *= reconnectionBackoffFactor
			if backoff > mc.reconnectionMaxInterval {
				backoff = mc.reconnectionMaxInterval
			}
		}
	}
	mc.logger.Error("Exceeded maximum reconnection attempts", zap.String("address", mc.addr))
}

// Close terminates the ManagedClient and its background routines.
func (mc *ManagedClient) Close() error {
	close(mc.quit)
	mc.wg.Wait()
	mc.disconnect()
	return nil
}

// isHealthy returns the current health status of the client.
func (mc *ManagedClient) isHealthy() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.healthy
}

// getClient safely retrieves the current ethclient.Client.
func (mc *ManagedClient) getClient() *ethclient.Client {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.client
}
