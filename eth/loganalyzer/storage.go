package loganalyzer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var ErrNotFound = errors.New("block logs not found")

// AllBlockLogs holds all three types of logs for a single block.
type AllBlockLogs struct {
	BatchLogs     []InternalLog
	FilterLogs    []InternalLog
	FinalizedLogs []InternalLog
}

const (
	storePrefix = "loganalyzer"

	// Block-specific data prefixes
	blockStatsKey       = "bs"
	problematicBlockKey = "pb"
	batchLogsKey        = "bl"
	filterLogsKey       = "fl"
	finalizedLogsKey    = "fnl"

	// Global state keys
	lastBatchBlockKey     = "lbb"
	lastFilterBlockKey    = "lfb"
	lastFinalizedBlockKey = "lf"
	lastProcessedKey      = "lpb"
)

// getKeyPrefix returns the storage key prefix for the given log type.
func (lt LogType) getKeyPrefix() string {
	switch lt {
	case BatchLogType:
		return batchLogsKey
	case FilterLogType:
		return filterLogsKey
	case FinalizedLogType:
		return finalizedLogsKey
	default:
		return ""
	}
}

// getLastBlockKey returns the storage key for tracking last received block by log type.
func (lt LogType) getLastBlockKey() string {
	switch lt {
	case BatchLogType:
		return lastBatchBlockKey
	case FilterLogType:
		return lastFilterBlockKey
	case FinalizedLogType:
		return lastFinalizedBlockKey
	default:
		return ""
	}
}

// LogAnalyzerStore interface for persisting log analysis data.
type LogAnalyzerStore interface {
	// Block analysis results
	SaveBlockStats(ctx context.Context, stats *BlockAnalysisStats) error
	GetBlockStats(ctx context.Context, blockNumber uint64) (*BlockAnalysisStats, error)
	GetBlockRange(ctx context.Context, fromBlock, toBlock uint64) ([]*BlockAnalysisStats, error)

	// Problematic blocks tracking
	SaveProblematicBlock(ctx context.Context, block *ProblematicBlock) error
	GetProblematicBlock(ctx context.Context, blockNumber uint64) (*ProblematicBlock, error)
	GetProblematicBlocks(ctx context.Context, startBlock uint64, endBlock *uint64, limit uint32) ([]*ProblematicBlock, bool, error)
	DeleteProblematicBlock(ctx context.Context, blockNumber uint64) error

	// Block data management
	DeleteBlockData(ctx context.Context, blockNumber uint64) error

	// Internal log storage
	SaveLogs(ctx context.Context, logType LogType, blockNumber uint64, logs []InternalLog) error
	SaveLastReceivedBlock(ctx context.Context, logType LogType, blockNumber uint64) error
	GetLastReceivedBlock(ctx context.Context, logType LogType) (uint64, error)
	GetLogs(ctx context.Context, logType LogType, blockNumber uint64) ([]InternalLog, error)
	GetAllLogs(ctx context.Context, blockNumber uint64) (*AllBlockLogs, error)
	DeleteLogs(ctx context.Context, blockNumber uint64) error

	// Processing state tracking
	SaveLastProcessedBlock(ctx context.Context, blockNumber uint64) error
	GetLastProcessedBlock(ctx context.Context) (uint64, error)
}

// BlockAnalysisStats represents the analysis results for a single block.
type BlockAnalysisStats struct {
	BlockNumber uint64 `json:"block_number"`

	// Counts from each source
	BatchLogCount     uint64 `json:"batch_log_count"`
	FilterLogCount    uint64 `json:"filter_log_count"`
	FinalizedLogCount uint64 `json:"finalized_log_count"`

	// Analysis results
	HasDiscrepancies bool   `json:"has_discrepancies"`
	MissingInBatch   uint64 `json:"missing_in_batch"`
	MissingInFilter  uint64 `json:"missing_in_filter"`
	ExtraInBatch     uint64 `json:"extra_in_batch"`
	ExtraInFilter    uint64 `json:"extra_in_filter"`

	// Metadata
	AnalyzedAt int64 `json:"analyzed_at"`
	IsFinal    bool  `json:"is_final"`
}

// LogReference identifies a specific log without storing full data.
type LogReference struct {
	LogIndex    uint     `json:"log_index"`
	TxHash      string   `json:"tx_hash"`
	Address     string   `json:"address"`
	TopicHashes []string `json:"topic_hashes,omitempty"`
}

// logAnalyzerStore implements LogAnalyzerStore using basedb.
type logAnalyzerStore struct {
	db basedb.Database
}

// NewLogAnalyzerStore creates a new log analyzer store.
func NewLogAnalyzerStore(db basedb.Database) LogAnalyzerStore {
	return &logAnalyzerStore{
		db: db,
	}
}

func (s *logAnalyzerStore) marshal(v interface{}) ([]byte, error) {
	return msgpack.Marshal(v)
}

func (s *logAnalyzerStore) unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

func (s *logAnalyzerStore) SaveBlockStats(ctx context.Context, stats *BlockAnalysisStats) error {
	key := s.makeKeyForBlock(blockStatsKey, stats.BlockNumber)

	value, err := s.marshal(stats)
	if err != nil {
		return fmt.Errorf("marshal block stats: %w", err)
	}

	return s.db.Set([]byte(storePrefix), key, value)
}

func (s *logAnalyzerStore) GetBlockStats(ctx context.Context, blockNumber uint64) (*BlockAnalysisStats, error) {
	key := s.makeKeyForBlock(blockStatsKey, blockNumber)

	obj, found, err := s.db.Get([]byte(storePrefix), key)
	if err != nil {
		return nil, fmt.Errorf("get block stats: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}

	var stats BlockAnalysisStats

	if err := s.unmarshal(obj.Value, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal block stats failed: %w", err)
	}

	return &stats, nil
}

func (s *logAnalyzerStore) DeleteBlockData(ctx context.Context, blockNumber uint64) error {
	keys := [][]byte{
		s.makeKeyForBlock(batchLogsKey, blockNumber),
		s.makeKeyForBlock(filterLogsKey, blockNumber),
		s.makeKeyForBlock(finalizedLogsKey, blockNumber),
		s.makeKeyForBlock(blockStatsKey, blockNumber),
		s.makeKeyForBlock(problematicBlockKey, blockNumber),
	}

	for _, key := range keys {
		if err := s.db.Delete([]byte(storePrefix), key); err != nil {
			return fmt.Errorf("delete block data for block %d: %w", blockNumber, err)
		}
	}

	return nil
}

func (s *logAnalyzerStore) GetBlockRange(ctx context.Context, fromBlock, toBlock uint64) ([]*BlockAnalysisStats, error) {
	results := make([]*BlockAnalysisStats, 0, toBlock-fromBlock+1)

	fromKey := s.makeKeyForBlock(blockStatsKey, fromBlock)
	toKey := s.makeKeyForBlock(blockStatsKey, toBlock)
	keyPrefixBytes := []byte(blockStatsKey)

	err := s.db.GetAll([]byte(storePrefix), func(i int, obj basedb.Obj) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Quick prefix check
		if len(obj.Key) < len(keyPrefixBytes) || !bytes.HasPrefix(obj.Key, keyPrefixBytes) {
			return nil
		}

		// Validate key length
		if len(obj.Key) != len(keyPrefixBytes)+8 {
			return nil
		}

		// Range check using binary comparison
		if bytes.Compare(obj.Key, fromKey) < 0 || bytes.Compare(obj.Key, toKey) > 0 {
			return nil
		}

		blockBytes := obj.Key[len(keyPrefixBytes):]
		blockNum := binary.LittleEndian.Uint64(blockBytes)

		if blockNum < fromBlock || blockNum > toBlock {
			return nil
		}

		var stats BlockAnalysisStats
		if err := s.unmarshal(obj.Value, &stats); err != nil {
			return fmt.Errorf("unmarshal block stats for block %d: %w", blockNum, err)
		}

		results = append(results, &stats)
		return nil
	})

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("block range query canceled (from=%d to=%d): %w", fromBlock, toBlock, err)
		}
		return nil, fmt.Errorf("iterate block range from=%d to=%d: %w", fromBlock, toBlock, err)
	}

	return results, nil
}

func (s *logAnalyzerStore) SaveProblematicBlock(ctx context.Context, block *ProblematicBlock) error {
	key := s.makeKeyForBlock(problematicBlockKey, block.BlockNumber)

	value, err := s.marshal(block)
	if err != nil {
		return fmt.Errorf("marshal problematic block: %w", err)
	}

	return s.db.Set([]byte(storePrefix), key, value)
}

func (s *logAnalyzerStore) GetProblematicBlocks(ctx context.Context, startBlock uint64, endBlock *uint64, limit uint32) ([]*ProblematicBlock, bool, error) {
	upperBound := endBlock
	if upperBound == nil {
		lastFinalized, err := s.GetLastReceivedBlock(ctx, FinalizedLogType)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return []*ProblematicBlock{}, false, nil
			}
			return nil, false, fmt.Errorf("get last finalized block: %w", err)
		}
		upperBound = &lastFinalized
	}

	results := make([]*ProblematicBlock, 0)
	hasMore := false

	fromKey := s.makeKeyForBlock(problematicBlockKey, startBlock)
	toKey := s.makeKeyForBlock(problematicBlockKey, *upperBound)
	keyPrefixBytes := []byte(problematicBlockKey)

	err := s.db.GetAll([]byte(storePrefix), func(i int, obj basedb.Obj) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if (limit > 0) && (len(results) >= int(limit)) {
			hasMore = true
			return nil
		}

		// Quick prefix check
		if len(obj.Key) < len(keyPrefixBytes) || !bytes.HasPrefix(obj.Key, keyPrefixBytes) {
			return nil
		}

		// Validate key length
		if len(obj.Key) != len(keyPrefixBytes)+8 {
			return nil
		}

		// Range check using binary comparison
		if bytes.Compare(obj.Key, fromKey) < 0 || bytes.Compare(obj.Key, toKey) > 0 {
			return nil
		}

		blockBytes := obj.Key[len(keyPrefixBytes):]
		blockNum := binary.LittleEndian.Uint64(blockBytes)

		if blockNum < startBlock || blockNum > *upperBound {
			return nil
		}

		var block ProblematicBlock
		if err := s.unmarshal(obj.Value, &block); err != nil {
			return fmt.Errorf("unmarshal problematic block for block %d: %w", blockNum, err)
		}

		results = append(results, &block)
		return nil
	})

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("problematic blocks query canceled (start=%d end=%v limit=%d): %w", startBlock, endBlock, limit, err)
		}
		return nil, false, fmt.Errorf("iterate problematic blocks start=%d end=%v limit=%d: %w", startBlock, endBlock, limit, err)
	}

	return results, hasMore, nil
}

func (s *logAnalyzerStore) DeleteProblematicBlock(ctx context.Context, blockNumber uint64) error {
	key := s.makeKeyForBlock(problematicBlockKey, blockNumber)
	return s.db.Delete([]byte(storePrefix), key)
}

// GetProblematicBlock retrieves a specific problematic block.
func (s *logAnalyzerStore) GetProblematicBlock(ctx context.Context, blockNumber uint64) (*ProblematicBlock, error) {
	key := s.makeKeyForBlock(problematicBlockKey, blockNumber)

	obj, found, err := s.db.Get([]byte(storePrefix), key)
	if err != nil {
		return nil, fmt.Errorf("get problematic block: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}

	var block ProblematicBlock

	if err := s.unmarshal(obj.Value, &block); err != nil {
		return nil, fmt.Errorf("unmarshal problematic block failed: %w", err)
	}

	return &block, nil
}

// SaveLogs saves logs for the specified type and block.
// For filter logs, appends to existing logs; for others, overwrites.
func (s *logAnalyzerStore) SaveLogs(ctx context.Context, logType LogType, blockNumber uint64, logs []InternalLog) error {
	if logType == FilterLogType {
		// For filter logs, append to existing logs
		existingLogs, err := s.GetLogs(ctx, FilterLogType, blockNumber)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		allLogs := append(existingLogs, logs...)
		return s.saveLogs(logType, blockNumber, allLogs)
	}
	// For batch and finalized logs, save directly
	return s.saveLogs(logType, blockNumber, logs)
}

// GetLastReceivedBlock retrieves the last received block number for the specified log type.
func (s *logAnalyzerStore) GetLastReceivedBlock(ctx context.Context, logType LogType) (uint64, error) {
	key := []byte(logType.getLastBlockKey())
	obj, found, err := s.db.Get([]byte(storePrefix), key)
	if err != nil {
		return 0, fmt.Errorf("get last received block for %s: %w", logType.String(), err)
	}
	if !found {
		return 0, ErrNotFound
	}

	var blockNumber uint64
	if err := s.unmarshal(obj.Value, &blockNumber); err != nil {
		return 0, fmt.Errorf("unmarshal last received block for %s: %w", logType.String(), err)
	}

	return blockNumber, nil
}

// SaveLastReceivedBlock saves the last received block number for the specified log type.
func (s *logAnalyzerStore) SaveLastReceivedBlock(ctx context.Context, logType LogType, blockNumber uint64) error {
	currentLast, err := s.GetLastReceivedBlock(ctx, logType)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("get current last received block for %s: %w", logType.String(), err)
	}

	// Only save if the new block number is higher
	if blockNumber > currentLast {
		key := []byte(logType.getLastBlockKey())
		value, err := s.marshal(blockNumber)
		if err != nil {
			return fmt.Errorf("marshal last received block for %s: %w", logType.String(), err)
		}
		return s.db.Set([]byte(storePrefix), key, value)
	}
	return nil
}

func (s *logAnalyzerStore) GetAllLogs(ctx context.Context, blockNumber uint64) (*AllBlockLogs, error) {
	result := &AllBlockLogs{}

	batchLogs, err := s.GetLogs(ctx, BatchLogType, blockNumber)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get batch logs: %w", err)
	}
	if batchLogs == nil {
		batchLogs = []InternalLog{}
	}
	result.BatchLogs = batchLogs

	filterLogs, err := s.GetLogs(ctx, FilterLogType, blockNumber)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get filter logs: %w", err)
	}
	if filterLogs == nil {
		filterLogs = []InternalLog{}
	}
	result.FilterLogs = filterLogs

	finalizedLogs, err := s.GetLogs(ctx, FinalizedLogType, blockNumber)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get finalized logs: %w", err)
	}
	if finalizedLogs == nil {
		finalizedLogs = []InternalLog{}
	}
	result.FinalizedLogs = finalizedLogs

	return result, nil
}

func (s *logAnalyzerStore) DeleteLogs(ctx context.Context, blockNumber uint64) error {
	prefixes := []string{batchLogsKey, filterLogsKey, finalizedLogsKey}

	for _, prefix := range prefixes {
		key := s.makeKeyForBlock(prefix, blockNumber)
		if err := s.db.Delete([]byte(storePrefix), key); err != nil {
			return fmt.Errorf("failed to delete %s logs for block %d: %w", prefix, blockNumber, err)
		}
	}

	return nil
}

func (s *logAnalyzerStore) SaveLastProcessedBlock(ctx context.Context, blockNumber uint64) error {
	key := []byte(lastProcessedKey)

	value, err := s.marshal(blockNumber)
	if err != nil {
		return fmt.Errorf("marshal last processed block: %w", err)
	}

	return s.db.Set([]byte(storePrefix), key, value)
}

func (s *logAnalyzerStore) GetLastProcessedBlock(ctx context.Context) (uint64, error) {
	key := []byte(lastProcessedKey)

	obj, found, err := s.db.Get([]byte(storePrefix), key)
	if err != nil {
		return 0, fmt.Errorf("get last processed block: %w", err)
	}
	if !found {
		return 0, ErrNotFound
	}

	var blockNumber uint64

	if err := s.unmarshal(obj.Value, &blockNumber); err != nil {
		return 0, fmt.Errorf("unmarshal last processed block failed: %w", err)
	}

	return blockNumber, nil
}

func (s *logAnalyzerStore) saveLogs(logType LogType, blockNumber uint64, logs []InternalLog) error {
	key := s.makeKeyForBlock(logType.getKeyPrefix(), blockNumber)

	value, err := s.marshal(logs)
	if err != nil {
		return fmt.Errorf("marshal %s logs: %w", logType.getKeyPrefix(), err)
	}

	return s.db.Set([]byte(storePrefix), key, value)
}

func (s *logAnalyzerStore) GetLogs(ctx context.Context, logType LogType, blockNumber uint64) ([]InternalLog, error) {
	key := s.makeKeyForBlock(logType.getKeyPrefix(), blockNumber)

	obj, found, err := s.db.Get([]byte(storePrefix), key)
	if err != nil {
		return nil, fmt.Errorf("get %s logs: %w", logType.getKeyPrefix(), err)
	}
	if !found {
		return nil, ErrNotFound
	}

	var logs []InternalLog

	if err := s.unmarshal(obj.Value, &logs); err != nil {
		return nil, fmt.Errorf("unmarshal %s logs failed: %w", logType.getKeyPrefix(), err)
	}

	return logs, nil
}

func (s *logAnalyzerStore) makeKeyForBlock(prefix string, blockNumber uint64) []byte {
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.LittleEndian.PutUint64(key[len(prefix):], blockNumber)
	return key
}
