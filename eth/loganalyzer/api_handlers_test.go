package loganalyzer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/eth/executionclient"
)

func TestAPIHandler_Health(t *testing.T) {
	handler, _, analyzer := testAPIHandler(t)

	finalizedLogs := executionclient.BlockLogs{
		BlockNumber: 100,
		Logs:        []ethtypes.Log{},
	}
	errCh := analyzer.RecordFinalizedLogs(finalizedLogs)
	require.NoError(t, <-errCh)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	err := handler.Health(rec, req)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	require.Equal(t, healthyStatus, response["status"])
	require.Equal(t, float64(100), response["lastFinalizedBlock"])

	require.Contains(t, response, "queueDepths")
	require.Contains(t, response, "processedLogCounts")
	require.Contains(t, response, "features")
}

// TestAPIHandler_ParameterValidation consolidates all parameter validation tests
func TestAPIHandler_ParameterValidation(t *testing.T) {
	handler, _, _ := testAPIHandler(t)

	invalidParamTests := []struct {
		name     string
		endpoint string
		query    string
	}{
		// Stats endpoint validation
		{"Stats_NoParams", "/stats", ""},
		{"Stats_InvalidBlock", "/stats", "?block=invalid"},
		{"Stats_InvalidFrom", "/stats", "?from=invalid&to=200"},
		{"Stats_InvalidTo", "/stats", "?from=100&to=invalid"},
		{"Stats_FromGreaterThanTo", "/stats", "?from=200&to=100"},
		{"Stats_RangeTooLarge", "/stats", "?from=100&to=1200"}, // >1000 range

		// ProblematicBlocks endpoint validation
		{"ProblematicBlocks_InvalidStartBlock", "/problematic-blocks", "?start_block=invalid"},
		{"ProblematicBlocks_InvalidEndBlock", "/problematic-blocks", "?start_block=100&end_block=invalid"},
		{"ProblematicBlocks_EndBeforeStart", "/problematic-blocks", "?start_block=100&end_block=50"},
		{"ProblematicBlocks_InvalidLimit", "/problematic-blocks", "?start_block=100&limit=invalid"},
	}

	for _, tt := range invalidParamTests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse(tt.endpoint + tt.query)
			req := httptest.NewRequest(http.MethodGet, u.String(), nil)
			rec := httptest.NewRecorder()

			var err error
			switch tt.endpoint {
			case "/stats":
				err = handler.Stats(rec, req)
			case "/problematic-blocks":
				err = handler.ProblematicBlocks(rec, req)
			}

			require.Error(t, err, "Expected error for %s", tt.name)
		})
	}
}

// TestAPIHandler_Stats consolidates all stats endpoint tests
func TestAPIHandler_Stats(t *testing.T) {
	handler, store, _ := testAPIHandler(t)
	ctx := context.Background()

	// Setup test data
	blockNumber := uint64(100)
	err := store.SaveLastReceivedBlock(ctx, FinalizedLogType, blockNumber)
	require.NoError(t, err)

	stats := &BlockAnalysisStats{
		BlockNumber:       blockNumber,
		BatchLogCount:     5,
		FilterLogCount:    4,
		FinalizedLogCount: 5,
		HasDiscrepancies:  true,
		MissingInBatch:    0,
		MissingInFilter:   1,
		ExtraInBatch:      0,
		ExtraInFilter:     0,
		AnalyzedAt:        time.Now().Unix(),
		IsFinal:           true,
	}
	err = store.SaveBlockStats(ctx, stats)
	require.NoError(t, err)

	// Setup range data for range tests
	for i := uint64(200); i <= 202; i++ {
		rangeStats := &BlockAnalysisStats{
			BlockNumber:       i,
			BatchLogCount:     i,
			FilterLogCount:    i,
			FinalizedLogCount: i,
			HasDiscrepancies:  i%2 == 0,
			AnalyzedAt:        time.Now().Unix(),
			IsFinal:           true,
		}
		err = store.SaveBlockStats(ctx, rangeStats)
		require.NoError(t, err)
	}

	tests := []struct {
		name           string
		query          string
		expectedStatus int
		validator      func(*testing.T, []byte)
	}{
		{
			name:           "SingleBlock",
			query:          "?block=100",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response BlockStatsResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.Len(t, response.BlockStats, 1)
				require.Equal(t, blockNumber, response.BlockStats[0].BlockNumber)
				require.True(t, response.BlockStats[0].HasDiscrepancies)
				require.NotNil(t, response.TransactionStats)
			},
		},
		{
			name:           "Latest",
			query:          "?block=latest",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response BlockStatsResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.Len(t, response.BlockStats, 1)
				require.Equal(t, blockNumber, response.BlockStats[0].BlockNumber)
			},
		},
		{
			name:           "BlockRange",
			query:          "?from=200&to=202",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response BlockStatsResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.Len(t, response.BlockStats, 3)
				require.Equal(t, uint64(200), response.BlockStats[0].BlockNumber)
				require.Equal(t, uint64(202), response.BlockStats[2].BlockNumber)
				require.NotNil(t, response.TransactionStats)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse("/stats" + tt.query)
			req := httptest.NewRequest(http.MethodGet, u.String(), nil)
			rec := httptest.NewRecorder()

			err := handler.Stats(rec, req)
			require.NoError(t, err)
			require.Equal(t, tt.expectedStatus, rec.Code)

			if tt.validator != nil {
				tt.validator(t, rec.Body.Bytes())
			}
		})
	}
}

func TestAPIHandler_Stats_ErrorCases(t *testing.T) {
	handler, _, _ := testAPIHandler(t)

	tests := []struct {
		name  string
		query string
	}{
		{"LatestBlock_NoData", "?block=latest"},
		{"BlockNotFound", "?block=999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse("/stats" + tt.query)
			req := httptest.NewRequest(http.MethodGet, u.String(), nil)
			rec := httptest.NewRecorder()

			err := handler.Stats(rec, req)
			require.Error(t, err)
		})
	}
}

// TestAPIHandler_ProblematicBlocks consolidates all problematic blocks endpoint tests
func TestAPIHandler_ProblematicBlocks(t *testing.T) {
	handler, store, _ := testAPIHandler(t)
	ctx := context.Background()

	// Setup test data
	blockNumber := uint64(100)
	err := store.SaveLastReceivedBlock(ctx, FinalizedLogType, blockNumber)
	require.NoError(t, err)

	problematicBlock := &ProblematicBlock{
		BlockNumber:        blockNumber,
		DetectedAt:         time.Now().Unix(),
		TotalDiscrepancies: 2,
	}
	err = store.SaveProblematicBlock(ctx, problematicBlock)
	require.NoError(t, err)

	tests := []struct {
		name           string
		query          string
		expectedStatus int
		validator      func(*testing.T, []byte)
	}{
		{
			name:           "Success",
			query:          "?start_block=100",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response ProblematicBlocksResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.Len(t, response.ProblematicBlocks, 1)
				require.Equal(t, blockNumber, response.ProblematicBlocks[0].BlockNumber)
				require.False(t, response.HasMore)
				require.Equal(t, blockNumber, response.LastFinalizedBlock)
				require.Equal(t, "problematic blocks after block 100", response.Description)
			},
		},
		{
			name:           "WithEndBlock",
			query:          "?start_block=100&end_block=200",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response ProblematicBlocksResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.Equal(t, blockNumber, response.LastFinalizedBlock)
				require.Equal(t, "problematic blocks between block 100 and 200", response.Description)
			},
		},
		{
			name:           "WithLimit",
			query:          "?start_block=100&limit=5",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response ProblematicBlocksResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.NotNil(t, response.ProblematicBlocks)
				require.Equal(t, blockNumber, response.LastFinalizedBlock)
				require.Equal(t, "problematic blocks after block 100", response.Description)
			},
		},
		{
			name:           "NoArguments_Default",
			query:          "",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response ProblematicBlocksResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.NotNil(t, response.ProblematicBlocks)
				require.Equal(t, blockNumber, response.LastFinalizedBlock)
				require.Equal(t, "last 10 problematic blocks", response.Description)
			},
		},
		{
			name:           "NoArguments_WithCustomLimit",
			query:          "?limit=20",
			expectedStatus: http.StatusOK,
			validator: func(t *testing.T, body []byte) {
				var response ProblematicBlocksResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				require.NotNil(t, response.ProblematicBlocks)
				require.Equal(t, blockNumber, response.LastFinalizedBlock)
				require.Equal(t, "last 20 problematic blocks", response.Description)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse("/problematic-blocks" + tt.query)
			req := httptest.NewRequest(http.MethodGet, u.String(), nil)
			rec := httptest.NewRecorder()

			err := handler.ProblematicBlocks(rec, req)
			require.NoError(t, err)
			require.Equal(t, tt.expectedStatus, rec.Code)

			if tt.validator != nil {
				tt.validator(t, rec.Body.Bytes())
			}
		})
	}
}

func TestComputeTransactionStats(t *testing.T) {
	blockNumber := uint64(100)

	// Create test logs with different transaction hashes
	batchLogs := []InternalLog{
		testInternalLog(blockNumber, 0, "0x1111", "0x1111111111111111111111111111111111111111111111111111111111111111"),
		testInternalLog(blockNumber, 1, "0x2222", "0x1111111111111111111111111111111111111111111111111111111111111111"), // tx1 has 2 logs in batch
		testInternalLog(blockNumber, 2, "0x3333", "0x2222222222222222222222222222222222222222222222222222222222222222"), // tx2 has 1 log in batch
	}

	filterLogs := []InternalLog{
		testInternalLog(blockNumber, 0, "0x1111", "0x1111111111111111111111111111111111111111111111111111111111111111"), // tx1 has 1 log in filter (missing 1)
		testInternalLog(blockNumber, 2, "0x3333", "0x2222222222222222222222222222222222222222222222222222222222222222"), // tx2 has 1 log in filter
		testInternalLog(blockNumber, 3, "0x4444", "0x3333333333333333333333333333333333333333333333333333333333333333"), // tx3 only in filter
	}

	finalizedLogs := []InternalLog{
		testInternalLog(blockNumber, 0, "0x1111", "0x1111111111111111111111111111111111111111111111111111111111111111"),
		testInternalLog(blockNumber, 1, "0x2222", "0x1111111111111111111111111111111111111111111111111111111111111111"), // tx1 should have 2 logs
		testInternalLog(blockNumber, 2, "0x3333", "0x2222222222222222222222222222222222222222222222222222222222222222"), // tx2 should have 1 log
		testInternalLog(blockNumber, 3, "0x4444", "0x3333333333333333333333333333333333333333333333333333333333333333"), // tx3 should have 1 log
	}

	txStats := ComputeTransactionStats(batchLogs, filterLogs, finalizedLogs)
	require.Len(t, txStats, 3) // Should have stats for tx1, tx2, tx3

	// Convert to map for easier testing
	statsMap := make(map[string]*TxLogStats)
	for _, stat := range txStats {
		statsMap[stat.TxHash] = stat
	}

	// Test tx1 (should have all logs in batch, missing 1 in filter)
	tx1Stats := statsMap["0x1111111111111111111111111111111111111111111111111111111111111111"]
	require.NotNil(t, tx1Stats)
	require.Equal(t, uint64(2), tx1Stats.ExpectedLogs)
	require.Equal(t, uint64(2), tx1Stats.BatchLogs)
	require.Equal(t, uint64(1), tx1Stats.FilterLogs)
	require.Equal(t, uint64(0), tx1Stats.MissingFromBatch)
	require.Equal(t, uint64(1), tx1Stats.MissingFromFilter)

	// Test tx2 (should have all logs in both)
	tx2Stats := statsMap["0x2222222222222222222222222222222222222222222222222222222222222222"]
	require.NotNil(t, tx2Stats)
	require.Equal(t, uint64(1), tx2Stats.ExpectedLogs)
	require.Equal(t, uint64(1), tx2Stats.BatchLogs)
	require.Equal(t, uint64(1), tx2Stats.FilterLogs)
	require.Equal(t, uint64(0), tx2Stats.MissingFromBatch)
	require.Equal(t, uint64(0), tx2Stats.MissingFromFilter)

	// Test tx3 (missing from batch, present in filter)
	tx3Stats := statsMap["0x3333333333333333333333333333333333333333333333333333333333333333"]
	require.NotNil(t, tx3Stats)
	require.Equal(t, uint64(1), tx3Stats.ExpectedLogs)
	require.Equal(t, uint64(0), tx3Stats.BatchLogs)
	require.Equal(t, uint64(1), tx3Stats.FilterLogs)
	require.Equal(t, uint64(1), tx3Stats.MissingFromBatch)
	require.Equal(t, uint64(0), tx3Stats.MissingFromFilter)
}
