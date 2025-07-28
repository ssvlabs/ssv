package loganalyzer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLogStorage_CRUD consolidates all log storage CRUD operations
func TestLogStorage_CRUD(t *testing.T) {
	store := testStore(t)
	blockNumber := uint64(100)

	// Test data
	logs := []InternalLog{
		testInternalLog(blockNumber, 0, "0x1111", "0xaaa1"),
		testInternalLog(blockNumber, 1, "0x2222", "0xaaa2"),
	}

	// Table-driven test for all log types
	tests := []struct {
		name    string
		logType LogType
	}{
		{
			name:    "BatchLogs",
			logType: BatchLogType,
		},
		{
			name:    "FilterLogs",
			logType: FilterLogType,
		},
		{
			name:    "FinalizedLogs",
			logType: FinalizedLogType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Test save and retrieve
			err := store.SaveLogs(ctx, tt.logType, blockNumber, logs)
			require.NoError(t, err)

			retrieved, err := store.GetLogs(ctx, tt.logType, blockNumber)
			require.NoError(t, err)
			require.Len(t, retrieved, 2)

			// Verify internal log details are preserved
			for i, log := range logs {
				require.Equal(t, log.BlockNumber, retrieved[i].BlockNumber)
				require.Equal(t, log.Index, retrieved[i].Index)
				require.Equal(t, log.Hash, retrieved[i].Hash)
				require.Equal(t, log.TxHash, retrieved[i].TxHash)
				require.Equal(t, log.Removed, retrieved[i].Removed)
			}

			// Test not found
			_, err = store.GetLogs(ctx, tt.logType, 999)
			require.ErrorIs(t, err, ErrNotFound)

			// Test overwrite (FilterLogType appends, others overwrite)
			newLogs := []InternalLog{testInternalLog(blockNumber, 10, "0x3333", "0xbbb1")}
			err = store.SaveLogs(ctx, tt.logType, blockNumber, newLogs)
			require.NoError(t, err)

			retrieved, err = store.GetLogs(ctx, tt.logType, blockNumber)
			require.NoError(t, err)

			if tt.logType == FilterLogType {
				// FilterLogType appends to existing logs
				require.Len(t, retrieved, 3)                         // original 2 + new 1
				require.Equal(t, newLogs[0].Hash, retrieved[2].Hash) // new log is last
			} else {
				// BatchLogType and FinalizedLogType overwrite
				require.Len(t, retrieved, 1)
				require.Equal(t, newLogs[0].Hash, retrieved[0].Hash)
			}
		})
	}
}

func TestLogAnalyzerStore_GetAllLogs(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	blockNumber := uint64(123)

	t.Run("EmptyData", func(t *testing.T) {
		// Test with no data
		allLogs, err := store.GetAllLogs(ctx, blockNumber)
		require.NoError(t, err)
		require.Empty(t, allLogs.BatchLogs)
		require.Empty(t, allLogs.FilterLogs)
		require.Empty(t, allLogs.FinalizedLogs)
	})

	t.Run("WithData", func(t *testing.T) {
		// Save some data and test again
		logs := []InternalLog{testInternalLog(blockNumber, 0, "0x1111", "0x2222")}

		err := store.SaveLogs(ctx, BatchLogType, blockNumber, logs)
		require.NoError(t, err)
		err = store.SaveLogs(ctx, FilterLogType, blockNumber, logs)
		require.NoError(t, err)
		err = store.SaveLogs(ctx, FinalizedLogType, blockNumber, logs)
		require.NoError(t, err)

		allLogs, err := store.GetAllLogs(ctx, blockNumber)
		require.NoError(t, err)
		require.Equal(t, logs, allLogs.BatchLogs)
		require.Equal(t, logs, allLogs.FilterLogs)
		require.Equal(t, logs, allLogs.FinalizedLogs)
	})
}

// TestStorage_CRUD consolidates all storage CRUD operations
func TestStorage_CRUD(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	t.Run("BlockStats", func(t *testing.T) {
		stats := &BlockAnalysisStats{
			BlockNumber:      500,
			BatchLogCount:    10,
			FilterLogCount:   8,
			HasDiscrepancies: true,
			AnalyzedAt:       time.Now().Unix(),
			IsFinal:          true,
		}

		// Save and retrieve
		err := store.SaveBlockStats(ctx, stats)
		require.NoError(t, err)

		retrieved, err := store.GetBlockStats(ctx, 500)
		require.NoError(t, err)
		require.Equal(t, stats.BlockNumber, retrieved.BlockNumber)
		require.Equal(t, stats.HasDiscrepancies, retrieved.HasDiscrepancies)

		// Test not found
		_, err = store.GetBlockStats(ctx, 999)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ProblematicBlocks", func(t *testing.T) {
		block := &ProblematicBlock{
			BlockNumber:        700,
			DetectedAt:         time.Now().Unix(),
			TotalDiscrepancies: 6,
		}

		// Save and retrieve
		err := store.SaveProblematicBlock(ctx, block)
		require.NoError(t, err)

		retrieved, err := store.GetProblematicBlock(ctx, 700)
		require.NoError(t, err)
		require.Equal(t, block.BlockNumber, retrieved.BlockNumber)
		require.Equal(t, block.TotalDiscrepancies, retrieved.TotalDiscrepancies)

		// Test not found
		_, err = store.GetProblematicBlock(ctx, 999)
		require.ErrorIs(t, err, ErrNotFound)

		// Test delete
		err = store.DeleteProblematicBlock(ctx, 700)
		require.NoError(t, err)

		_, err = store.GetProblematicBlock(ctx, 700)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("LastBlocks", func(t *testing.T) {
		// Test LastFinalizedBlock
		_, err := store.GetLastReceivedBlock(ctx, FinalizedLogType)
		require.ErrorIs(t, err, ErrNotFound)

		err = store.SaveLastReceivedBlock(ctx, FinalizedLogType, 12345)
		require.NoError(t, err)

		lastFinalized, err := store.GetLastReceivedBlock(ctx, FinalizedLogType)
		require.NoError(t, err)
		require.Equal(t, uint64(12345), lastFinalized)

		// Test LastProcessedBlock
		_, err = store.GetLastProcessedBlock(ctx)
		require.ErrorIs(t, err, ErrNotFound)

		err = store.SaveLastProcessedBlock(ctx, 67890)
		require.NoError(t, err)

		lastProcessed, err := store.GetLastProcessedBlock(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(67890), lastProcessed)
	})
}

func TestLogAnalyzerStore_BlockRange(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Create test data
	for i := uint64(600); i <= 605; i++ {
		stats := &BlockAnalysisStats{
			BlockNumber:      i,
			BatchLogCount:    i * 2,
			HasDiscrepancies: i%2 == 0,
		}
		err := store.SaveBlockStats(ctx, stats)
		require.NoError(t, err)
	}

	// Test range
	retrieved, err := store.GetBlockRange(ctx, 602, 604)
	require.NoError(t, err)
	require.Len(t, retrieved, 3) // 602, 603, 604
	require.Equal(t, uint64(602), retrieved[0].BlockNumber)
	require.Equal(t, uint64(604), retrieved[2].BlockNumber)

	// Test empty range
	retrieved, err = store.GetBlockRange(ctx, 700, 702)
	require.NoError(t, err)
	require.Len(t, retrieved, 0)
}

func TestLogAnalyzerStore_ProblematicBlocks_List(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Set last finalized block (required for GetProblematicBlocks)
	err := store.SaveLastReceivedBlock(ctx, FinalizedLogType, 110)
	require.NoError(t, err)

	// Prepare test data
	for i := uint64(100); i <= 105; i++ {
		block := &ProblematicBlock{
			BlockNumber:        i,
			DetectedAt:         time.Now().Unix(),
			TotalDiscrepancies: i,
		}
		err := store.SaveProblematicBlock(ctx, block)
		require.NoError(t, err)
	}

	// Test GetProblematicBlocks with various parameters
	blocks, hasMore, err := store.GetProblematicBlocks(ctx, 100, nil, 10)
	require.NoError(t, err)
	require.Len(t, blocks, 6) // All blocks from 100-105
	require.False(t, hasMore)

	// Test with limit
	blocks, hasMore, err = store.GetProblematicBlocks(ctx, 100, nil, 3)
	require.NoError(t, err)
	require.Len(t, blocks, 3)
	require.True(t, hasMore)

	// Test with end block
	endBlock := uint64(103)
	blocks, hasMore, err = store.GetProblematicBlocks(ctx, 100, &endBlock, 10)
	require.NoError(t, err)
	require.Len(t, blocks, 4) // 100, 101, 102, 103
	require.False(t, hasMore)
}

// TestStorage_EdgeCases consolidates edge case testing
func TestStorage_EdgeCases(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	blockNumber := uint64(1000)

	t.Run("EmptyData", func(t *testing.T) {
		// Empty logs should work
		err := store.SaveLogs(ctx, BatchLogType, blockNumber, []InternalLog{})
		require.NoError(t, err)

		logs, err := store.GetLogs(ctx, BatchLogType, blockNumber)
		require.NoError(t, err)
		require.Len(t, logs, 0)

		// Nil logs should work
		err = store.SaveLogs(ctx, FilterLogType, blockNumber, nil)
		require.NoError(t, err)

		logs, err = store.GetLogs(ctx, FilterLogType, blockNumber)
		require.NoError(t, err)
		require.Len(t, logs, 0)
	})

	t.Run("DeleteOperations", func(t *testing.T) {
		logs := []InternalLog{testInternalLog(blockNumber, 0, "0x1111", "0x2222")}
		stats := &BlockAnalysisStats{
			BlockNumber:       blockNumber,
			BatchLogCount:     1,
			FilterLogCount:    1,
			FinalizedLogCount: 1,
			AnalyzedAt:        time.Now().Unix(),
			IsFinal:           true,
		}
		problemBlock := &ProblematicBlock{
			BlockNumber:        blockNumber,
			DetectedAt:         time.Now().Unix(),
			TotalDiscrepancies: 1,
		}

		// Save all types of data
		err := store.SaveLogs(ctx, BatchLogType, blockNumber, logs)
		require.NoError(t, err)
		err = store.SaveLogs(ctx, FilterLogType, blockNumber, logs)
		require.NoError(t, err)
		err = store.SaveLogs(ctx, FinalizedLogType, blockNumber, logs)
		require.NoError(t, err)
		err = store.SaveBlockStats(ctx, stats)
		require.NoError(t, err)
		err = store.SaveProblematicBlock(ctx, problemBlock)
		require.NoError(t, err)

		// Delete logs only
		err = store.DeleteLogs(ctx, blockNumber)
		require.NoError(t, err)

		// Verify log deletion
		_, err = store.GetLogs(ctx, BatchLogType, blockNumber)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = store.GetLogs(ctx, FilterLogType, blockNumber)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = store.GetLogs(ctx, FinalizedLogType, blockNumber)
		require.ErrorIs(t, err, ErrNotFound)

		// Other data should still exist
		_, err = store.GetBlockStats(ctx, blockNumber)
		require.NoError(t, err)
		_, err = store.GetProblematicBlock(ctx, blockNumber)
		require.NoError(t, err)

		// Delete all block data
		err = store.DeleteBlockData(ctx, blockNumber)
		require.NoError(t, err)

		// Verify all data is gone
		_, err = store.GetBlockStats(ctx, blockNumber)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = store.GetProblematicBlock(ctx, blockNumber)
		require.ErrorIs(t, err, ErrNotFound)

		// Test deleting non-existent data (should not error)
		err = store.DeleteLogs(ctx, 9999)
		require.NoError(t, err)
		err = store.DeleteBlockData(ctx, 9999)
		require.NoError(t, err)
	})

	t.Run("LargeData", func(t *testing.T) {
		largeBlockNumber := uint64(1200)

		// Create large internal log array
		logs := make([]InternalLog, 1000)
		for i := 0; i < 1000; i++ {
			logs[i] = testInternalLog(largeBlockNumber, uint(i), "0x1234", "0xabcd")
		}

		// Test saving and retrieving large data
		err := store.SaveLogs(ctx, BatchLogType, largeBlockNumber, logs)
		require.NoError(t, err)

		retrieved, err := store.GetLogs(ctx, BatchLogType, largeBlockNumber)
		require.NoError(t, err)
		require.Len(t, retrieved, 1000)

		// Verify first and last logs
		require.Equal(t, uint(0), retrieved[0].Index)
		require.Equal(t, uint(999), retrieved[999].Index)
	})
}
