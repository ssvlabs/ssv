package loganalyzer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
)

const (
	MaxAPIRange   = 1000 // Maximum allowed block range for API queries
	healthyStatus = "healthy"
)

// ParameterError represents a parameter validation error.
type ParameterError struct {
	Parameter string
	Value     string
	Reason    string
}

func (e ParameterError) Error() string {
	return fmt.Sprintf("invalid parameter '%s' with value '%s': %s", e.Parameter, e.Value, e.Reason)
}

func (h *APIHandler) parseBlockNumber(value string) (uint64, error) {
	if value == "latest" {
		blockNumber, err := h.store.GetLastReceivedBlock(context.Background(), FinalizedLogType)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return 0, ParameterError{Parameter: "block", Value: value, Reason: "no finalized blocks available yet"}
			}
			return 0, fmt.Errorf("failed to get last finalized block: %w", err)
		}
		return blockNumber, nil
	}

	blockNumber, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, ParameterError{Parameter: "block", Value: value, Reason: err.Error()}
	}
	return blockNumber, nil
}

func parseUint64Parameter(paramName, value string) (uint64, error) {
	result, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, ParameterError{Parameter: paramName, Value: value, Reason: err.Error()}
	}
	return result, nil
}

func parseUint32Parameter(paramName, value string) (uint32, error) {
	result, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, ParameterError{Parameter: paramName, Value: value, Reason: err.Error()}
	}
	return uint32(result), nil
}

func validateBlockRange(fromBlock, toBlock uint64) error {
	if fromBlock > toBlock {
		return ParameterError{Parameter: "range", Value: fmt.Sprintf("%d-%d", fromBlock, toBlock), Reason: "from block cannot be greater than to block"}
	}

	if toBlock-fromBlock > MaxAPIRange {
		return ParameterError{Parameter: "range", Value: fmt.Sprintf("%d-%d", fromBlock, toBlock), Reason: fmt.Sprintf("range too large, maximum allowed range is %d blocks", MaxAPIRange)}
	}

	return nil
}

// LogAnalyzerProvider interface for accessing LogAnalyzer health status.
type LogAnalyzerProvider interface {
	GetHealthStatus() HealthStatus
}

// APIHandler provides HTTP endpoints for log analysis queries.
type APIHandler struct {
	logger   *zap.Logger
	store    LogAnalyzerStore
	analyzer LogAnalyzerProvider
}

// NewAPIHandler creates a new API handler for log analysis.
func NewAPIHandler(logger *zap.Logger, store LogAnalyzerStore, analyzer LogAnalyzerProvider) *APIHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if store == nil {
		panic("store cannot be nil")
	}
	if analyzer == nil {
		panic("analyzer cannot be nil - use the enhanced LogAnalyzer for comprehensive health monitoring")
	}

	return &APIHandler{
		logger:   logger.Named("logAnalyzerAPI"),
		store:    store,
		analyzer: analyzer,
	}
}

// Stats handles requests for block analysis statistics.
// Supports both single blocks (?block=123) and ranges (?from=100&to=200).
func (h *APIHandler) Stats(w http.ResponseWriter, r *http.Request) error {
	blockParam := r.URL.Query().Get("block")
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")

	if blockParam != "" {
		return h.handleSingleBlock(w, r, blockParam)
	} else if fromParam != "" && toParam != "" {
		return h.handleBlockRange(w, r, fromParam, toParam)
	} else {
		return api.BadRequestError(errors.New("must specify either 'block' or 'from'+'to' parameters"))
	}
}

func (h *APIHandler) handleSingleBlock(w http.ResponseWriter, r *http.Request, blockParam string) error {
	blockNumber, err := h.parseBlockNumber(blockParam)
	if err != nil {
		var paramErr ParameterError
		if errors.As(err, &paramErr) {
			return api.BadRequestError(paramErr)
		}
		h.logger.Error("failed to parse block number", zap.Error(err))
		return api.Error(err)
	}

	stats, err := h.store.GetBlockStats(r.Context(), blockNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return api.ErrNotFound
		}
		h.logger.Error("failed to get block stats", zap.Uint64("block", blockNumber), zap.Error(err))
		return api.Error(fmt.Errorf("failed to get block stats for block %d: %w", blockNumber, err))
	}

	allLogs, err := h.store.GetAllLogs(r.Context(), blockNumber)
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.logger.Error("failed to get all logs for transaction stats", zap.Uint64("block", blockNumber), zap.Error(err))
	}

	var txStats []*TxLogStats
	if err == nil {
		txStats = ComputeTransactionStats(allLogs.BatchLogs, allLogs.FilterLogs, allLogs.FinalizedLogs)
	} else {
		txStats = []*TxLogStats{}
	}

	return api.Render(w, r, &BlockStatsResponse{
		BlockStats:       []*BlockAnalysisStats{stats},
		TransactionStats: txStats,
		HasMore:          false,
	})
}

func (h *APIHandler) handleBlockRange(w http.ResponseWriter, r *http.Request, fromParam, toParam string) error {
	fromBlock, err := parseUint64Parameter("from", fromParam)
	if err != nil {
		return api.BadRequestError(err)
	}

	toBlock, err := parseUint64Parameter("to", toParam)
	if err != nil {
		return api.BadRequestError(err)
	}

	if err := validateBlockRange(fromBlock, toBlock); err != nil {
		return api.BadRequestError(err)
	}

	stats, err := h.store.GetBlockRange(r.Context(), fromBlock, toBlock)
	if err != nil {
		h.logger.Error("failed to get block range stats",
			zap.Uint64("from", fromBlock),
			zap.Uint64("to", toBlock),
			zap.Error(err))
		return api.Error(fmt.Errorf("failed to get block range stats from %d to %d: %w", fromBlock, toBlock, err))
	}

	// Compute transaction stats for blocks with discrepancies
	var allTxStats []*TxLogStats
	for _, blockStats := range stats {
		if blockStats.HasDiscrepancies {
			allLogs, err := h.store.GetAllLogs(r.Context(), blockStats.BlockNumber)
			if err != nil {
				h.logger.Debug("could not get logs for transaction stats",
					zap.Uint64("block", blockStats.BlockNumber),
					zap.Error(err))
				continue
			}
			txStats := ComputeTransactionStats(allLogs.BatchLogs, allLogs.FilterLogs, allLogs.FinalizedLogs)
			allTxStats = append(allTxStats, txStats...)
		}
	}

	if allTxStats == nil {
		allTxStats = []*TxLogStats{}
	}

	return api.Render(w, r, &BlockStatsResponse{
		BlockStats:       stats,
		TransactionStats: allTxStats,
		HasMore:          false,
	})
}

// ProblematicBlocks handles requests for blocks with log discrepancies.
func (h *APIHandler) ProblematicBlocks(w http.ResponseWriter, r *http.Request) error {
	lastFinalized, err := h.store.GetLastReceivedBlock(r.Context(), FinalizedLogType)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			lastFinalized = 0
		} else {
			h.logger.Error("failed to get last finalized block", zap.Error(err))
			return api.Error(err)
		}
	}

	startBlockParam := r.URL.Query().Get("start_block")
	endBlockParam := r.URL.Query().Get("end_block")
	limitParam := r.URL.Query().Get("limit")

	var startBlock uint64
	var endBlock *uint64
	var limit uint32 = 100
	var description string

	// Handle case where no start_block is provided
	if startBlockParam == "" {
		limit = 10
		if limitParam != "" {
			limitVal, err := parseUint32Parameter("limit", limitParam)
			if err != nil {
				return api.BadRequestError(err)
			}
			limit = limitVal
		}

		startBlock = 0
		description = fmt.Sprintf("last %d problematic blocks", limit)
	} else {
		var err error
		startBlock, err = parseUint64Parameter("start_block", startBlockParam)
		if err != nil {
			return api.BadRequestError(err)
		}

		if endBlockParam != "" {
			endBlockVal, err := parseUint64Parameter("end_block", endBlockParam)
			if err != nil {
				return api.BadRequestError(err)
			}
			endBlock = &endBlockVal

			if *endBlock < startBlock {
				return api.BadRequestError(ParameterError{Parameter: "end_block", Value: endBlockParam, Reason: "end_block cannot be less than start_block"})
			}
		}

		if limitParam != "" {
			limitVal, err := parseUint32Parameter("limit", limitParam)
			if err != nil {
				return api.BadRequestError(err)
			}
			limit = limitVal
		}

		if endBlock != nil {
			description = fmt.Sprintf("problematic blocks between block %d and %d", startBlock, *endBlock)
		} else {
			description = fmt.Sprintf("problematic blocks after block %d", startBlock)
		}
	}

	blocks, hasMore, err := h.store.GetProblematicBlocks(r.Context(), startBlock, endBlock, limit)
	if err != nil {
		h.logger.Error("failed to get problematic blocks",
			zap.Uint64("start_block", startBlock),
			zap.Any("end_block", endBlock),
			zap.Uint32("limit", limit),
			zap.Error(err))
		return api.Error(fmt.Errorf("failed to get problematic blocks (start=%d end=%v limit=%d): %w", startBlock, endBlock, limit, err))
	}

	return api.Render(w, r, &ProblematicBlocksResponse{
		ProblematicBlocks:  blocks,
		HasMore:            hasMore,
		LastFinalizedBlock: lastFinalized,
		Description:        description,
	})
}

// Health provides comprehensive health check and status summary.
func (h *APIHandler) Health(w http.ResponseWriter, r *http.Request) error {
	// TODO fix possible data race since we access analyzer stats
	health := h.analyzer.GetHealthStatus()

	// Additional storage verification
	_, err := h.store.GetLastReceivedBlock(r.Context(), FinalizedLogType)
	if err != nil && !errors.Is(err, ErrNotFound) {
		health.Status = "unhealthy"
		h.logger.Error("storage health check failed", zap.Error(err))
	} else if errors.Is(err, ErrNotFound) && health.Status == healthyStatus {
		health.Status = "no_data"
	}

	// Add queue health warnings
	if health.Status == healthyStatus {
		for queueType, depth := range health.QueueDepths {
			maxQueueSize := 5000 // Default from constants
			if float64(depth)/float64(maxQueueSize) > 0.8 {
				health.Status = "degraded"
				h.logger.Warn("queue depth high",
					zap.String("queue_type", queueType),
					zap.Int("depth", depth))
			}
		}
	}

	return api.Render(w, r, health)
}
