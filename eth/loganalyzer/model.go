// Package loganalyzer provides analysis and comparison of blockchain logs from multiple sources.
// It tracks discrepancies between batch logs, filter logs, and finalized logs to detect
// potential issues in log processing pipelines.
package loganalyzer

import (
	"encoding/binary"
	"encoding/hex"
	"hash"
	"hash/fnv"
	"sync"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/ssvlabs/ssv/eth/executionclient"
)

// LogType represents the different sources of logs being processed.
type LogType int

const (
	BatchLogType LogType = iota
	FilterLogType
	FinalizedLogType
)

func (lt LogType) String() string {
	switch lt {
	case BatchLogType:
		return "batch logs"
	case FilterLogType:
		return "filter logs"
	case FinalizedLogType:
		return "finalized logs"
	default:
		return "unknown"
	}
}

// hasherPool reuses hash instances for better performance in concurrent processing.
var hasherPool = sync.Pool{
	New: func() interface{} {
		return fnv.New64()
	},
}

// InternalLog represents an optimized log structure for analysis and comparison.
type InternalLog struct {
	BlockNumber uint64 `json:"block_number"`
	Index       uint   `json:"index"`   // Log index within block
	Hash        string `json:"hash"`    // Content-based hash for reliable comparison
	TxHash      string `json:"tx_hash"` // Transaction hash for tracking
	Removed     bool   `json:"removed"` // Whether this log was removed
}

// computeLogHash creates a content-based hash for reliable log comparison.
// Uses FNV64 for performance while maintaining good distribution properties.
func computeLogHash(log ethtypes.Log) string {
	hasher := hasherPool.Get().(hash.Hash64)
	defer hasherPool.Put(hasher)
	hasher.Reset()

	// Hash address, topics, data, and transaction hash
	_, _ = hasher.Write(log.Address.Bytes())
	for _, topic := range log.Topics {
		_, _ = hasher.Write(topic.Bytes())
	}
	_, _ = hasher.Write(log.Data)
	_, _ = hasher.Write(log.TxHash.Bytes())

	hashBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(hashBytes, hasher.Sum64())
	return hex.EncodeToString(hashBytes)
}

// InternalBlockLogs represents a block of internal logs.
type InternalBlockLogs struct {
	BlockNumber uint64        `json:"block_number"`
	Logs        []InternalLog `json:"logs"`
}

// ToInternalLog converts an ethereum log to our internal representation.
func ToInternalLog(log ethtypes.Log) InternalLog {
	return InternalLog{
		BlockNumber: log.BlockNumber,
		Index:       log.Index,
		Hash:        computeLogHash(log),
		TxHash:      log.TxHash.Hex(),
		Removed:     log.Removed,
	}
}

// ToInternalBlockLogs converts executionclient.BlockLogs to our internal representation.
func ToInternalBlockLogs(blockLogs executionclient.BlockLogs) InternalBlockLogs {
	internalLogs := make([]InternalLog, len(blockLogs.Logs))
	for i, log := range blockLogs.Logs {
		internalLogs[i] = ToInternalLog(log)
	}
	return InternalBlockLogs{
		BlockNumber: blockLogs.BlockNumber,
		Logs:        internalLogs,
	}
}

// ExtractLogHashes returns a set of log hashes for comparison.
func ExtractLogHashes(logs []InternalLog) map[string]struct{} {
	hashes := make(map[string]struct{}, len(logs))
	for _, log := range logs {
		hashes[log.Hash] = struct{}{}
	}
	return hashes
}

// CountDiscrepanciesByHash compares log hashes and returns discrepancy counts.
func CountDiscrepanciesByHash(batchHashes, filterHashes, finalizedHashes map[string]struct{}) (missingInBatch, missingInFilter, extraInBatch, extraInFilter uint64) {
	// Count logs missing in batch (present in finalized but not in batch)
	for hash := range finalizedHashes {
		if _, found := batchHashes[hash]; !found {
			missingInBatch++
		}
	}

	// Count logs missing in filter (present in finalized but not in filter)
	for hash := range finalizedHashes {
		if _, found := filterHashes[hash]; !found {
			missingInFilter++
		}
	}

	// Count extra logs in batch (present in batch but not in finalized)
	for hash := range batchHashes {
		if _, found := finalizedHashes[hash]; !found {
			extraInBatch++
		}
	}

	// Count extra logs in filter (present in filter but not in finalized)
	for hash := range filterHashes {
		if _, found := finalizedHashes[hash]; !found {
			extraInFilter++
		}
	}

	return missingInBatch, missingInFilter, extraInBatch, extraInFilter
}

// ComputeTransactionStats aggregates logs by transaction hash and computes per-transaction statistics.
func ComputeTransactionStats(batchLogs, filterLogs, finalizedLogs []InternalLog) []*TxLogStats {
	// Group logs by transaction hash
	txMap := make(map[string]*TxLogStats)

	// Process finalized logs (ground truth)
	for _, log := range finalizedLogs {
		if _, exists := txMap[log.TxHash]; !exists {
			txMap[log.TxHash] = &TxLogStats{
				TxHash: log.TxHash,
			}
		}
		txMap[log.TxHash].ExpectedLogs++
	}

	// Process batch logs
	for _, log := range batchLogs {
		if _, exists := txMap[log.TxHash]; !exists {
			txMap[log.TxHash] = &TxLogStats{
				TxHash: log.TxHash,
			}
		}
		txMap[log.TxHash].BatchLogs++
	}

	// Process filter logs
	for _, log := range filterLogs {
		if _, exists := txMap[log.TxHash]; !exists {
			txMap[log.TxHash] = &TxLogStats{
				TxHash: log.TxHash,
			}
		}
		txMap[log.TxHash].FilterLogs++
	}

	// Compute missing counts
	for _, stats := range txMap {
		if stats.BatchLogs < stats.ExpectedLogs {
			stats.MissingFromBatch = stats.ExpectedLogs - stats.BatchLogs
		}
		if stats.FilterLogs < stats.ExpectedLogs {
			stats.MissingFromFilter = stats.ExpectedLogs - stats.FilterLogs
		}
	}

	// Convert to slice
	result := make([]*TxLogStats, 0, len(txMap))
	for _, stats := range txMap {
		result = append(result, stats)
	}

	return result
}

// StorageConfig defines the storage configuration.
type StorageConfig struct {
	RetainBlocks uint64 `yaml:"retain_blocks"`
}

// BlockStatsResponse represents the API response for block statistics.
type BlockStatsResponse struct {
	BlockStats       []*BlockAnalysisStats `json:"block_stats"`
	TransactionStats []*TxLogStats         `json:"transaction_stats"`
	HasMore          bool                  `json:"has_more"`
}

// ProblematicBlock represents a block with log discrepancies.
type ProblematicBlock struct {
	BlockNumber        uint64 `json:"block_number"`
	DetectedAt         int64  `json:"detected_at"`
	TotalDiscrepancies uint64 `json:"total_discrepancies"`
}

// ProblematicBlocksResponse represents the API response for problematic blocks.
type ProblematicBlocksResponse struct {
	ProblematicBlocks  []*ProblematicBlock `json:"problematic_blocks"`
	HasMore            bool                `json:"has_more"`
	LastFinalizedBlock uint64              `json:"last_finalized_block"`
	Description        string              `json:"description"`
}

// TxLogStats represents log statistics per transaction hash.
type TxLogStats struct {
	TxHash            string `json:"tx_hash"`
	ExpectedLogs      uint64 `json:"expected_logs"`
	BatchLogs         uint64 `json:"batch_logs"`
	FilterLogs        uint64 `json:"filter_logs"`
	MissingFromBatch  uint64 `json:"missing_from_batch"`
	MissingFromFilter uint64 `json:"missing_from_filter"`
}

// ProcessorRequest represents a request that can be processed by the analyzer.
type ProcessorRequest interface {
	Process(analyzer *LogAnalyzer) error
	GetResponseChannel() chan error
}
