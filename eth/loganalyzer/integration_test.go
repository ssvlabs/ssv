package loganalyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/executionclient"
)

func TestLogAnalyzer_IntegrationScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create logAnalyzer with in-memory storage
	config := Config{
		MaxQueueSize: 1000,
		Storage: StorageConfig{
			RetainBlocks: 100,
		},
	}
	analyzer := New(context.Background(), logger, nil, config)
	require.NotNil(t, analyzer)
	defer analyzer.Close()

	// Create API handler
	apiHandler := NewAPIHandler(logger, analyzer.GetStore(), analyzer)

	// Test scenario blocks
	emptyBlock1 := uint64(1000)      // Empty block (no logs)
	normalBlock := uint64(1001)      // Normal block (matching logs)
	emptyBlock2 := uint64(1002)      // Another empty block
	problematicBlock := uint64(1003) // Problematic block (missing logs)
	emptyBlock3 := uint64(1004)      // Final empty block

	// Create test logs for normal block (all sources match)
	normalLog1 := ethtypes.Log{
		Address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Topics:      []common.Hash{common.HexToHash("0xabcd")},
		Data:        []byte("normal data 1"),
		BlockNumber: normalBlock,
		TxHash:      common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		Index:       0,
		Removed:     false,
	}

	normalLog2 := ethtypes.Log{
		Address:     common.HexToAddress("0x9876543210987654321098765432109876543210"),
		Topics:      []common.Hash{common.HexToHash("0xefgh")},
		Data:        []byte("normal data 2"),
		BlockNumber: normalBlock,
		TxHash:      common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		Index:       1,
		Removed:     false,
	}

	// Create test logs for problematic block
	// Batch and finalized will have both logs, but filter will miss one
	problematicLog1 := ethtypes.Log{
		Address:     common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Topics:      []common.Hash{common.HexToHash("0x1234")},
		Data:        []byte("problematic data 1"),
		BlockNumber: problematicBlock,
		TxHash:      common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		Index:       0,
		Removed:     false,
	}

	problematicLog2 := ethtypes.Log{
		Address:     common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		Topics:      []common.Hash{common.HexToHash("0x5678")},
		Data:        []byte("problematic data 2"),
		BlockNumber: problematicBlock,
		TxHash:      common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
		Index:       1,
		Removed:     false,
	}

	// Simulate processing empty blocks
	for _, blockNum := range []uint64{emptyBlock1, emptyBlock2, emptyBlock3} {
		emptyBlockLogs := executionclient.BlockLogs{
			BlockNumber: blockNum,
			Logs:        []ethtypes.Log{}, // No logs
		}

		// Process through all sources
		<-analyzer.RecordBlockLogs(emptyBlockLogs)
		<-analyzer.RecordFinalizedLogs(emptyBlockLogs)
	}

	// Simulate processing normal block (all sources have same logs)
	normalBlockLogs := executionclient.BlockLogs{
		BlockNumber: normalBlock,
		Logs:        []ethtypes.Log{normalLog1, normalLog2},
	}

	// Process batch logs
	<-analyzer.RecordBlockLogs(normalBlockLogs)

	// Process filter logs
	<-analyzer.RecordFilterLogs(normalLog1)
	<-analyzer.RecordFilterLogs(normalLog2)

	// Process finalized logs (this triggers comparison)
	<-analyzer.RecordFinalizedLogs(normalBlockLogs)

	// Simulate processing problematic block (filter source missing one log)
	problematicBlockLogs := executionclient.BlockLogs{
		BlockNumber: problematicBlock,
		Logs:        []ethtypes.Log{problematicLog1, problematicLog2},
	}

	// Process batch logs
	<-analyzer.RecordBlockLogs(problematicBlockLogs)

	// Filter source only has first log (missing second log to create discrepancy)
	<-analyzer.RecordFilterLogs(problematicLog1)
	// Note: We deliberately don't send problematicLog2 to filter source

	// Process finalized logs (this triggers comparison and detects discrepancy)
	<-analyzer.RecordFinalizedLogs(problematicBlockLogs)

	// Test 1: Check stats for empty block - should return success with zero counts
	t.Run("EmptyBlock_ReturnsZeroStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/stats?block=%d", emptyBlock1), nil)
		rec := httptest.NewRecorder()

		err := apiHandler.Stats(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response BlockStatsResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response.BlockStats, 1)
		stats := response.BlockStats[0]
		require.Equal(t, emptyBlock1, stats.BlockNumber)
		require.Equal(t, uint64(0), stats.BatchLogCount)
		require.Equal(t, uint64(0), stats.FilterLogCount)
		require.Equal(t, uint64(0), stats.FinalizedLogCount)
		require.False(t, stats.HasDiscrepancies)
		require.True(t, stats.IsFinal)
	})

	// Test 2: Check stats for normal block - should return success with no discrepancies
	t.Run("NormalBlock_ReturnsStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/stats?block=%d", normalBlock), nil)
		rec := httptest.NewRecorder()

		err := apiHandler.Stats(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response BlockStatsResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response.BlockStats, 1)
		stats := response.BlockStats[0]
		require.Equal(t, normalBlock, stats.BlockNumber)
		require.Equal(t, uint64(2), stats.BatchLogCount)
		require.Equal(t, uint64(2), stats.FilterLogCount)
		require.Equal(t, uint64(2), stats.FinalizedLogCount)
		require.False(t, stats.HasDiscrepancies)
		require.Equal(t, uint64(0), stats.MissingInBatch)
		require.Equal(t, uint64(0), stats.MissingInFilter)
		require.True(t, stats.IsFinal)
	})

	// Test 3: Check stats for problematic block - should show discrepancies
	t.Run("ProblematicBlock_ShowsDiscrepancies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/stats?block=%d", problematicBlock), nil)
		rec := httptest.NewRecorder()

		err := apiHandler.Stats(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response BlockStatsResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response.BlockStats, 1)
		stats := response.BlockStats[0]
		require.Equal(t, problematicBlock, stats.BlockNumber)
		require.Equal(t, uint64(2), stats.BatchLogCount)
		require.Equal(t, uint64(1), stats.FilterLogCount) // Only first log sent to filter
		require.Equal(t, uint64(2), stats.FinalizedLogCount)
		require.True(t, stats.HasDiscrepancies)
		require.Equal(t, uint64(0), stats.MissingInBatch)
		require.Equal(t, uint64(1), stats.MissingInFilter) // One log missing in filter
		require.True(t, stats.IsFinal)

		// Transaction stats should be initialized (can be empty if no logs found)
		require.NotNil(t, response.TransactionStats)
	})

	// Test 4: Check stats for block range - should include both normal and problematic blocks
	t.Run("BlockRange_ReturnsMultipleStats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/stats?from=%d&to=%d", emptyBlock1, problematicBlock), nil)
		rec := httptest.NewRecorder()

		err := apiHandler.Stats(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response BlockStatsResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.GreaterOrEqual(t, len(response.BlockStats), 2) // Should have at least our test blocks
		require.False(t, response.HasMore)

		// Find each block in results
		var normalStats, problematicStats *BlockAnalysisStats
		for _, stats := range response.BlockStats {
			if stats.BlockNumber == normalBlock {
				normalStats = stats
			} else if stats.BlockNumber == problematicBlock {
				problematicStats = stats
			}
		}

		require.NotNil(t, normalStats, "Should find normal block stats")
		require.NotNil(t, problematicStats, "Should find problematic block stats")
		require.False(t, normalStats.HasDiscrepancies, "Normal block should have no discrepancies")
		require.True(t, problematicStats.HasDiscrepancies, "Problematic block should have discrepancies")
	})

	// Test 5: Check problematic blocks endpoint - should return problematic blocks
	t.Run("ProblematicBlocks_ReturnsProblematicBlocks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/problematic-blocks?start_block=%d&end_block=%d", emptyBlock1, emptyBlock3), nil)
		rec := httptest.NewRecorder()

		err := apiHandler.ProblematicBlocks(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response ProblematicBlocksResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.GreaterOrEqual(t, len(response.ProblematicBlocks), 1) // At least one problematic block
		require.False(t, response.HasMore)

		// Find our specific problematic block
		var found bool
		for _, block := range response.ProblematicBlocks {
			if block.BlockNumber == problematicBlock {
				found = true
				require.Greater(t, block.TotalDiscrepancies, uint64(0))
				require.Greater(t, block.DetectedAt, int64(0))
				break
			}
		}
		require.True(t, found, "Should find our problematic block in results")
	})

	// Test 6: Check problematic blocks with no results - should return empty array
	t.Run("ProblematicBlocks_EmptyRange_ReturnsEmptyArray", func(t *testing.T) {
		// Query a range before our test blocks
		req := httptest.NewRequest(http.MethodGet, "/problematic-blocks?start_block=500&end_block=600", nil)
		rec := httptest.NewRecorder()

		err := apiHandler.ProblematicBlocks(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response ProblematicBlocksResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.NotNil(t, response.ProblematicBlocks) // Should be empty array, not nil
		require.Len(t, response.ProblematicBlocks, 0)
		require.False(t, response.HasMore)

		// Verify JSON structure
		jsonStr := rec.Body.String()
		require.Contains(t, jsonStr, `"problematic_blocks":[]`) // Empty array, not null
	})

	// Test 7: Check health endpoint
	t.Run("Health_ReturnsHealthyStatus", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		err := apiHandler.Health(rec, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Equal(t, "healthy", response["status"])
		require.Contains(t, response, "lastFinalizedBlock")

		// Verify comprehensive health data
		require.Contains(t, response, "queueDepths")
		require.Contains(t, response, "processedLogCounts")
		require.Contains(t, response, "headToFinalizedLag")

		features := response["features"].(map[string]interface{})
		require.True(t, features["storage"].(bool))
		require.True(t, features["problematic_blocks"].(bool))
		require.True(t, features["block_analysis"].(bool))
		require.True(t, features["queue_monitoring"].(bool))
	})
}
