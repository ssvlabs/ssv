package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ssvlabs/ssv/cli"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/logging"
	"go.uber.org/zap"
)

// AppName is the application name
var AppName = "SSV-Node"

// Version is the app version
var Version = "latest"

// Commit is the git commit this version was built on
var Commit = "unknown"

// Test function to verify consistent event counting across goroutines
func testConcurrentLogStreaming() {
	// Setup
	ctx, cancel := context.WithTimeout(context.Background(), 72*time.Hour)
	defer cancel()

	// Configuration - hardcoded for testing
	ethNodeURL := "ws://host.docker.internal:8546"
	contractAddr := "0xdac17f958d2ee523a2206206994597c13d831ec7" // USDT mainnet contract

	// Setup logger
	if err := logging.SetGlobalLogger("info", "capital", "console", nil); err != nil {
		log.Fatalf("Failed to setup logger: %v", err)
	}
	logger := zap.L()

	// Thread-safe map to track block events
	type blockEventCount struct {
		mu     sync.RWMutex
		counts map[uint64]int
	}
	eventTracker := &blockEventCount{
		counts: make(map[uint64]int),
	}

	// Channel to report critical issues
	criticalIssues := make(chan string, 1000)

	// Start issue reporter
	go func() {
		for issue := range criticalIssues {
			logger.Error("CRITICAL ISSUE DETECTED", zap.String("issue", issue))
		}
	}()

	// Wait group for goroutines
	var wg sync.WaitGroup

	// First, create a temporary client to get the current block
	logger.Info("Fetching current block number...")
	tempClient, err := executionclient.New(
		ctx,
		ethNodeURL,
		common.HexToAddress(contractAddr),
		executionclient.WithLogger(logger),
		executionclient.WithConnectionTimeout(10*time.Second),
	)
	if err != nil {
		logger.Fatal("Failed to create temporary execution client", zap.Error(err))
	}

	// Get the current block
	latestHeader, err := tempClient.HeaderByNumber(ctx, nil)
	if err != nil {
		logger.Fatal("Failed to fetch current block", zap.Error(err))
	}
	startBlock := latestHeader.Number.Uint64()
	logger.Info("Starting from current block", zap.Uint64("startBlock", startBlock))

	// Launch 20 goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			logger.Info("Starting goroutine", zap.Int("routineID", routineID))

			// Create execution client for this goroutine
			logger.Info("Creating execution client",
				zap.String("contractAddr", contractAddr),
				zap.String("ethNodeURL", ethNodeURL),
				zap.Uint64("startBlock", startBlock))

			client, err := executionclient.New(
				ctx,
				ethNodeURL,
				common.HexToAddress(contractAddr),
				executionclient.WithLogger(logger.With(zap.Int("routineID", routineID))),
				executionclient.WithFollowDistance(8),
				executionclient.WithConnectionTimeout(10*time.Second),
			)
			if err != nil {
				logger.Error("Failed to create execution client",
					zap.Int("routineID", routineID),
					zap.Error(err))
				return
			}

			// Stream logs
			logStream := client.StreamLogs(ctx, startBlock)

			for blockLogs := range logStream {
				eventCount := len(blockLogs.Logs)
				blockNumber := blockLogs.BlockNumber

				// Check and update event count
				eventTracker.mu.Lock()
				existingCount, exists := eventTracker.counts[blockNumber]
				if exists && existingCount != eventCount {
					// Critical issue detected!
					issue := fmt.Sprintf(
						"Block %d event count mismatch! Routine %d found %d events, but %d were previously recorded",
						blockNumber, routineID, eventCount, existingCount,
					)
					criticalIssues <- issue
					logger.Error("CRITICAL: Event count mismatch detected!",
						zap.Int("routineID", routineID),
						zap.Uint64("blockNumber", blockNumber),
						zap.Int("thisCount", eventCount),
						zap.Int("previousCount", existingCount))
					panic(fmt.Sprintf("Event count mismatch! Block %d: routine %d found %d events, but %d were previously recorded", blockNumber, routineID, eventCount, existingCount))
				} else {
					eventTracker.counts[blockNumber] = eventCount
				}
				eventTracker.mu.Unlock()

				// Log progress
				if blockNumber%10 == 0 { // More frequent logging for testing
					logger.Info("Processing block",
						zap.Int("routineID", routineID),
						zap.Uint64("blockNumber", blockNumber),
						zap.Int("eventCount", eventCount))
				}
			}

			logger.Info("Goroutine finished", zap.Int("routineID", routineID))
		}(i)
	}

	// Monitor progress
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				eventTracker.mu.RLock()
				blockCount := len(eventTracker.counts)
				eventTracker.mu.RUnlock()

				logger.Info("Progress update",
					zap.Int("blocksProcessed", blockCount))
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for all goroutines or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All goroutines completed")
	case <-ctx.Done():
		logger.Warn("Test timed out")
	}

	close(criticalIssues)

	// Print final statistics
	eventTracker.mu.RLock()
	totalBlocks := len(eventTracker.counts)
	totalEvents := 0
	for _, count := range eventTracker.counts {
		totalEvents += count
	}
	eventTracker.mu.RUnlock()

	logger.Info("Test completed",
		zap.Int("totalBlocksProcessed", totalBlocks),
		zap.Int("totalEventsFound", totalEvents))
}

func main() {
	// Check if running in test mode
	if len(os.Args) > 1 && os.Args[1] == "test-concurrent-logs" {
		testConcurrentLogStreaming()
		return
	}

	// Normal SSV node execution
	version := fmt.Sprintf("%s-%s", Version, Commit)
	cli.Execute(AppName, version)
}
