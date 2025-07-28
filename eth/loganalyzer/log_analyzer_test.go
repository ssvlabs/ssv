package loganalyzer

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestLogAnalyzer_New(t *testing.T) {
	analyzer := testLogAnalyzer(t)

	require.NotNil(t, analyzer)
	require.NotNil(t, analyzer.logger)
	require.NotNil(t, analyzer.logsQueues)
	require.NotNil(t, analyzer.logsQueues[BatchLogType].channel)
	require.NotNil(t, analyzer.logsQueues[FilterLogType].channel)
	require.NotNil(t, analyzer.logsQueues[FinalizedLogType].channel)
	require.NotNil(t, analyzer.store)
	require.NotNil(t, analyzer.GetStore())
}

// TestLogAnalyzer_CoreFunctionality tests the essential LogAnalyzer operations
func TestLogAnalyzer_CoreFunctionality(t *testing.T) {
	analyzer := testLogAnalyzer(t)

	t.Run("InitializationAndCleanup", func(t *testing.T) {
		// Verify proper initialization
		require.NotNil(t, analyzer.logger)
		require.NotNil(t, analyzer.logsQueues[BatchLogType].channel)
		require.NotNil(t, analyzer.logsQueues[FilterLogType].channel)
		require.NotNil(t, analyzer.logsQueues[FinalizedLogType].channel)
		require.NotNil(t, analyzer.store)
		require.NotNil(t, analyzer.GetStore())

		// Test idempotent close
		errCh1 := analyzer.Close()
		errCh2 := analyzer.Close()

		<-errCh1 // Wait for first close
		<-errCh2 // Second close should complete immediately

		// Context should be canceled
		select {
		case <-analyzer.ctx.Done():
			// Expected
		default:
			t.Fatal("Context not canceled after Close()")
		}
	})

	// Create fresh analyzer for remaining tests since previous one was closed
	analyzer = testLogAnalyzer(t)

	t.Run("QueueProcessing", func(t *testing.T) {
		blockLogs := simpleTestBlockLogs(100)
		log := simpleTestLog(100)

		// Test that all queue operations complete without blocking
		errCh1 := analyzer.RecordBlockLogs(blockLogs)
		errCh2 := analyzer.RecordFilterLogs(log)
		errCh3 := analyzer.RecordFinalizedLogs(blockLogs)

		// Wait for all processing to complete
		<-errCh1
		<-errCh2
		<-errCh3

		// Verify queues are processed (empty)
		require.Equal(t, 0, len(analyzer.logsQueues[BatchLogType].channel))
		require.Equal(t, 0, len(analyzer.logsQueues[FilterLogType].channel))
		require.Equal(t, 0, len(analyzer.logsQueues[FinalizedLogType].channel))
	})

	t.Run("FilterLogBatching", func(t *testing.T) {
		blockNumber := uint64(500)

		// Add multiple filter logs
		var errChannels []<-chan error
		for i := 0; i < 10; i++ {
			log := testLog(blockNumber, uint(i), "0x1234", "0xabcd")
			errCh := analyzer.RecordFilterLogs(log)
			errChannels = append(errChannels, errCh)
		}

		// Wait for all logs to be processed
		for _, errCh := range errChannels {
			<-errCh
		}

		// Verify storage (no flush needed since we save immediately)

		savedLogs, err := analyzer.store.GetLogs(context.Background(), FilterLogType, blockNumber)
		require.NoError(t, err)
		require.Len(t, savedLogs, 10)
	})
}

func TestLogAnalyzer_FilterLogBatchingLarge(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Storage:      StorageConfig{RetainBlocks: 1000},
		MaxQueueSize: 5000,
	}
	analyzer := New(context.Background(), logger, nil, config)
	require.NotNil(t, analyzer)
	defer analyzer.Close()

	// Add multiple filter logs for the same block
	blockNumber := uint64(500)
	var errChannels []<-chan error
	for i := 0; i < 50; i++ {
		log := testLog(blockNumber, uint(i), "0x1234", "0xabcd")
		errCh := analyzer.RecordFilterLogs(log)
		errChannels = append(errChannels, errCh)
	}

	// Wait for all logs to be processed
	for _, errCh := range errChannels {
		<-errCh
	}

	// No flush needed since we save logs immediately

	// Verify logs were saved to storage
	savedLogs, err := analyzer.store.GetLogs(context.Background(), FilterLogType, blockNumber)
	require.NoError(t, err)
	require.Equal(t, 50, len(savedLogs), "All filter logs should be saved to storage")
}

// TestLogAnalyzer_HashComparison tests the hash-based log comparison logic
func TestLogAnalyzer_HashComparison(t *testing.T) {
	blockNumber := uint64(600)

	// Create logs with same content but different indices
	log1 := ethtypes.Log{
		Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Topics:      []common.Hash{common.HexToHash("0xabcd")},
		Data:        []byte("test data"),
		BlockNumber: blockNumber,
		TxHash:      common.HexToHash("0x1111"),
		Index:       10, // Different index
	}

	log2 := ethtypes.Log{
		Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Topics:      []common.Hash{common.HexToHash("0xabcd")},
		Data:        []byte("test data"),
		BlockNumber: blockNumber,
		TxHash:      common.HexToHash("0x1111"),
		Index:       99, // Different index, same content
	}

	// Convert to internal representation
	internal1 := ToInternalLog(log1)
	internal2 := ToInternalLog(log2)

	t.Run("SameContentSameHash", func(t *testing.T) {
		// Logs with same content should have same hash despite different indices
		require.Equal(t, internal1.Hash, internal2.Hash)
		require.NotEqual(t, internal1.Index, internal2.Index)
	})

	t.Run("HashBasedComparison", func(t *testing.T) {
		hashes1 := ExtractLogHashes([]InternalLog{internal1})
		hashes2 := ExtractLogHashes([]InternalLog{internal2})

		// Should be considered the same log
		missing, _, extra, _ := CountDiscrepanciesByHash(hashes1, hashes1, hashes2)
		require.Equal(t, uint64(0), missing)
		require.Equal(t, uint64(0), extra)
	})

	t.Run("DifferentContentDifferentHash", func(t *testing.T) {
		differentLog := ethtypes.Log{
			Address:     common.HexToAddress("0x9999999999999999999999999999999999999999"),
			Topics:      []common.Hash{common.HexToHash("0xefgh")},
			Data:        []byte("different data"),
			BlockNumber: blockNumber,
			TxHash:      common.HexToHash("0x2222"),
			Index:       10,
		}

		internal3 := ToInternalLog(differentLog)
		require.NotEqual(t, internal1.Hash, internal3.Hash)

		hashes1 := ExtractLogHashes([]InternalLog{internal1})
		hashes3 := ExtractLogHashes([]InternalLog{internal3})

		missing, _, extra, _ := CountDiscrepanciesByHash(hashes1, hashes1, hashes3)
		require.Equal(t, uint64(1), missing)
		require.Equal(t, uint64(1), extra)
	})
}

func TestLogAnalyzer_HashBasedComparisonDetailed(t *testing.T) {
	blockNumber := uint64(600)

	// Create test logs with same content but different indices
	testLogs := []ethtypes.Log{
		{
			Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Topics:      []common.Hash{common.HexToHash("0xabcd")},
			Data:        []byte("test data 1"),
			BlockNumber: blockNumber,
			TxHash:      common.HexToHash("0x1111"),
			Index:       10, // Different index
		},
		{
			Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Topics:      []common.Hash{common.HexToHash("0xabcd")},
			Data:        []byte("test data 1"),
			BlockNumber: blockNumber,
			TxHash:      common.HexToHash("0x1111"),
			Index:       99, // Different index, same content
		},
	}

	// Convert to internal representation
	internal1 := ToInternalLog(testLogs[0])
	internal2 := ToInternalLog(testLogs[1])

	// Verify that logs with same content have same hash despite different indices
	require.Equal(t, internal1.Hash, internal2.Hash, "Logs with same content should have same hash")
	require.NotEqual(t, internal1.Index, internal2.Index, "Test logs should have different indices")

	// Test hash-based comparison
	hashes1 := ExtractLogHashes([]InternalLog{internal1})
	hashes2 := ExtractLogHashes([]InternalLog{internal2})

	// Should be considered the same log for discrepancy analysis
	missing, _, extra, _ := CountDiscrepanciesByHash(hashes1, hashes1, hashes2)
	require.Equal(t, uint64(0), missing, "No logs should be missing when comparing same content")
	require.Equal(t, uint64(0), extra, "No extra logs when comparing same content")

	// Test with different content
	differentLog := ethtypes.Log{
		Address:     common.HexToAddress("0x9999999999999999999999999999999999999999"),
		Topics:      []common.Hash{common.HexToHash("0xefgh")},
		Data:        []byte("different data"),
		BlockNumber: blockNumber,
		TxHash:      common.HexToHash("0x2222"),
		Index:       10, // Same index as first log
	}

	internal3 := ToInternalLog(differentLog)
	require.NotEqual(t, internal1.Hash, internal3.Hash, "Logs with different content should have different hashes")

	hashes3 := ExtractLogHashes([]InternalLog{internal3})
	missing, _, extra, _ = CountDiscrepanciesByHash(hashes1, hashes1, hashes3)
	require.Equal(t, uint64(1), missing, "Should detect missing log when content differs")
	require.Equal(t, uint64(1), extra, "Should detect extra log when content differs")
}
