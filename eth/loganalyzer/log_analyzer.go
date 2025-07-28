package loganalyzer

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	DefaultMaxQueueSize       = 5000
	DefaultRetentionBlocks    = 1000
	QueueFullWarningThreshold = 0.8
)

// LogAnalyzerStats holds internal statistics for the LogAnalyzer.
type LogAnalyzerStats struct {
	LogCount              map[LogType]uint64
	LastReceivedBlock     map[LogType]uint64
	LastEvictedBlock      uint64
	StorageErrors         uint64
	ProblematicBlockCount uint64
}

// HealthStatus provides comprehensive health information for the LogAnalyzer.
type HealthStatus struct {
	Status             string            `json:"status"`
	LastFinalizedBlock uint64            `json:"lastFinalizedBlock"`
	LastReceivedBlocks map[string]uint64 `json:"lastReceivedBlocks"`
	QueueDepths        map[string]int    `json:"queueDepths"`
	QueueDroppedCounts map[string]uint64 `json:"queueDroppedCounts"`
	ProcessedLogCounts map[string]uint64 `json:"processedLogCounts"`
	StorageErrors      uint64            `json:"storageErrors"`
	ProblematicBlocks  uint64            `json:"problematicBlocks"`
	LastEvictedBlock   uint64            `json:"lastEvictedBlock"`
	HeadToFinalizedLag uint64            `json:"headToFinalizedLag"`
	IsEnabled          bool              `json:"isEnabled"`
	Features           map[string]bool   `json:"features"`
}

func (s *LogAnalyzerStats) increaseCounter(logType LogType, logCount uint64) {
	s.LogCount[logType] += logCount
}

func (s *LogAnalyzerStats) updateLastReceivedBlock(logType LogType, blockNumber uint64) {
	if blockNumber > s.LastReceivedBlock[logType] {
		s.LastReceivedBlock[logType] = blockNumber
	}
}

// LogRequest represents a unified request for all log types.
type LogRequest struct {
	logs       any
	Type       LogType
	ResponseCh chan error
}

type CloseRequest struct {
	ResponseCh chan error
}

type Queue struct {
	channel        chan LogRequest
	droppedCounter *atomic.Uint64
	logDescription string
}

func newQueue(maxSize int, description string) Queue {
	return Queue{
		channel:        make(chan LogRequest, maxSize),
		droppedCounter: &atomic.Uint64{},
		logDescription: description,
	}
}

func (q *Queue) tryEnqueue(request LogRequest, logger *zap.Logger) error {
	select {
	case q.channel <- request:
		return nil
	default:
		q.droppedCounter.Add(1)
		logger.Warn(q.logDescription+" queue full, dropping logs",
			zap.String("log_type", q.logDescription),
			zap.Uint64("total_dropped", q.droppedCounter.Load()))
		return errors.New("queue full")
	}
}

func (q *Queue) depth() int {
	return len(q.channel)
}

func (q *Queue) dropped() uint64 {
	return q.droppedCounter.Load()
}

// LogAnalyzer provides thread-safe analysis of blockchain logs from multiple sources.
// Designed for high-throughput log processing with minimal back pressure.
type LogAnalyzer struct {
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	enabled       bool
	store         LogAnalyzerStore
	ownedDB       basedb.Database
	storageConfig StorageConfig
	maxQueueSize  int
	closed        atomic.Bool

	// Statistics - accessed only by processor goroutine
	stats LogAnalyzerStats

	// Request queues
	logsQueues   map[LogType]Queue
	closeRequest chan CloseRequest
}

// Config holds configuration for the LogAnalyzer.
type Config struct {
	MaxQueueSize int           `yaml:"maxQueueSize" env:"LOG_ANALYZER_MAX_QUEUE_SIZE" env-default:"5000"`
	Storage      StorageConfig `yaml:"storage"`
}

// New creates a new LogAnalyzer instance.
func New(ctx context.Context, logger *zap.Logger, db basedb.Database, config Config) *LogAnalyzer {
	if ctx == nil {
		ctx = context.Background()
		logger.Debug("context is nil, using background context")
	}
	if logger == nil {
		return nil
	}

	// Set defaults
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = DefaultMaxQueueSize
	}
	if config.Storage.RetainBlocks == 0 {
		config.Storage.RetainBlocks = DefaultRetentionBlocks
	}

	var store LogAnalyzerStore
	var ownedDB basedb.Database

	if db == nil {
		inMemoryDB, err := badger.NewInMemory(logger.Named("logAnalyzer.db"), basedb.Options{})
		if err != nil {
			logger.Error("failed to create in-memory badger database for log analyzer", zap.Error(err))
			return nil
		}
		store = NewLogAnalyzerStore(inMemoryDB)
		ownedDB = inMemoryDB
		logger.Debug("created in-memory store for log analyzer")
	} else {
		store = NewLogAnalyzerStore(db)
		logger.Debug("using provided database for log analyzer")
	}

	analyzerCtx, cancel := context.WithCancel(ctx)
	analyzer := &LogAnalyzer{
		logger:        logger.Named("logAnalyzer"),
		maxQueueSize:  config.MaxQueueSize,
		logsQueues:    make(map[LogType]Queue),
		ctx:           analyzerCtx,
		cancel:        cancel,
		enabled:       true,
		store:         store,
		ownedDB:       ownedDB,
		storageConfig: config.Storage,
		closeRequest:  make(chan CloseRequest, 1),
		stats: LogAnalyzerStats{
			LogCount:          make(map[LogType]uint64),
			LastReceivedBlock: make(map[LogType]uint64),
		},
	}

	// Initialize queues for each log type
	for _, logType := range []LogType{BatchLogType, FilterLogType, FinalizedLogType} {
		analyzer.logsQueues[logType] = newQueue(config.MaxQueueSize, logType.String())
	}

	go analyzer.processor()

	return analyzer
}

// GetStore returns the LogAnalyzer's store for API access.
func (l *LogAnalyzer) GetStore() LogAnalyzerStore {
	return l.store
}

// GetHealthStatus returns comprehensive health information about the LogAnalyzer.
func (l *LogAnalyzer) GetHealthStatus() HealthStatus {
	status := "healthy"
	if l.closed.Load() {
		status = "closed"
	} else if !l.enabled {
		status = "disabled"
	} else if l.stats.StorageErrors > 0 {
		status = "degraded"
	}

	// Calculate head to finalized lag
	var lastReceived uint64
	for _, blockNum := range l.stats.LastReceivedBlock {
		if blockNum > lastReceived {
			lastReceived = blockNum
		}
	}
	lastFinalized := l.stats.LastReceivedBlock[FinalizedLogType]
	var headToFinalizedLag uint64
	if lastReceived >= lastFinalized {
		headToFinalizedLag = lastReceived - lastFinalized
	}

	// Build maps with string keys for JSON
	lastReceivedBlocks := make(map[string]uint64)
	for logType, blockNum := range l.stats.LastReceivedBlock {
		lastReceivedBlocks[logType.String()] = blockNum
	}

	queueDepths := make(map[string]int)
	queueDroppedCounts := make(map[string]uint64)
	for logType, queue := range l.logsQueues {
		queueDepths[logType.String()] = queue.depth()
		queueDroppedCounts[logType.String()] = queue.dropped()
	}

	processedLogCounts := make(map[string]uint64)
	for logType, count := range l.stats.LogCount {
		processedLogCounts[logType.String()] = count
	}

	return HealthStatus{
		Status:             status,
		LastFinalizedBlock: lastFinalized,
		LastReceivedBlocks: lastReceivedBlocks,
		QueueDepths:        queueDepths,
		QueueDroppedCounts: queueDroppedCounts,
		ProcessedLogCounts: processedLogCounts,
		StorageErrors:      l.stats.StorageErrors,
		ProblematicBlocks:  l.stats.ProblematicBlockCount,
		LastEvictedBlock:   l.stats.LastEvictedBlock,
		HeadToFinalizedLag: headToFinalizedLag,
		IsEnabled:          l.enabled,
		Features: map[string]bool{
			"storage":            true,
			"problematic_blocks": true,
			"block_analysis":     true,
			"queue_monitoring":   true,
		},
	}
}

// Close gracefully shuts down the LogAnalyzer.
func (l *LogAnalyzer) Close() <-chan error {
	errCh := make(chan error, 1)

	if !l.closed.CompareAndSwap(false, true) {
		close(errCh)
		return errCh
	}

	request := CloseRequest{
		ResponseCh: errCh,
	}

	select {
	case l.closeRequest <- request:
	default:
		l.cancel()
		close(errCh)
	}

	return errCh
}

// RecordBlockLogs processes logs received from batch operations.
func (l *LogAnalyzer) RecordBlockLogs(blockLogs executionclient.BlockLogs) <-chan error {
	l.logger.Debug("recordBlockLogs called",
		zap.Uint64("block", blockLogs.BlockNumber),
		zap.Int("log_count", len(blockLogs.Logs)))

	internalBlockLogs := ToInternalBlockLogs(blockLogs)
	return l.recordLogs(BatchLogType, &internalBlockLogs)
}

// RecordFilterLogs processes logs received from filter subscriptions.
func (l *LogAnalyzer) RecordFilterLogs(log ethtypes.Log) <-chan error {
	l.logger.Debug("recordFilterLogs called",
		zap.Uint64("block", log.BlockNumber),
		zap.Uint("log_index", log.Index))

	internalLog := ToInternalLog(log)
	internalBlockLogs := InternalBlockLogs{
		BlockNumber: log.BlockNumber,
		Logs:        []InternalLog{internalLog},
	}
	return l.recordLogs(FilterLogType, &internalBlockLogs)
}

// RecordFinalizedLogs processes logs received from finalized blocks.
func (l *LogAnalyzer) RecordFinalizedLogs(blockLogs executionclient.BlockLogs) <-chan error {
	l.logger.Debug("recordFinalizedLogs called",
		zap.Uint64("block", blockLogs.BlockNumber),
		zap.Int("log_count", len(blockLogs.Logs)))

	internalBlockLogs := ToInternalBlockLogs(blockLogs)
	return l.recordLogs(FinalizedLogType, &internalBlockLogs)
}

func (l *LogAnalyzer) recordLogs(logType LogType, logs any) <-chan error {
	errCh := make(chan error, 1)

	if !l.enabled {
		close(errCh)
		return errCh
	}

	request := LogRequest{
		Type:       logType,
		logs:       logs,
		ResponseCh: errCh,
	}

	queue := l.logsQueues[logType]
	if err := queue.tryEnqueue(request, l.logger); err != nil {
		errCh <- err
	}

	return errCh
}

// processor is the single goroutine that processes all log messages.
func (l *LogAnalyzer) processor() {
	for {
		select {
		case <-l.ctx.Done():
			return

		case request := <-l.closeRequest:
			l.handleCloseWithResponse(request)
			return

		case request := <-l.logsQueues[BatchLogType].channel:
			l.processLogRequest(request)

		case request := <-l.logsQueues[FilterLogType].channel:
			l.processLogRequest(request)

		case request := <-l.logsQueues[FinalizedLogType].channel:
			l.processLogRequest(request)
		}
	}
}

func (l *LogAnalyzer) handleCloseWithResponse(request CloseRequest) {
	defer func() {
		if r := recover(); r != nil {
			select {
			case request.ResponseCh <- errors.New("close panic"):
			default:
			}
			return
		}
		close(request.ResponseCh)
	}()

	l.cancel()

	for _, queue := range l.logsQueues {
		close(queue.channel)
	}
	close(l.closeRequest)

	if l.ownedDB != nil {
		if err := l.ownedDB.Close(); err != nil {
			l.logger.Warn("failed to close database", zap.Error(err))
		}
	}
}

func (l *LogAnalyzer) processLogRequest(request LogRequest) {
	defer func() {
		if r := recover(); r != nil {
			select {
			case request.ResponseCh <- errors.New("processing panic"):
			default:
			}
			return
		}
		close(request.ResponseCh)
	}()

	select {
	case <-l.ctx.Done():
		select {
		case request.ResponseCh <- l.ctx.Err():
		default:
		}
		return
	default:
	}

	start := time.Now()

	internalBlockLogs := request.logs.(*InternalBlockLogs)
	blockNumber := internalBlockLogs.BlockNumber
	logs := internalBlockLogs.Logs

	if err := l.store.SaveLastReceivedBlock(l.ctx, request.Type, blockNumber); err != nil {
		l.logger.Error("failed to save last received block for "+request.Type.String(),
			zap.Uint64("block", blockNumber),
			zap.Error(err))
	}

	select {
	case <-l.ctx.Done():
		select {
		case request.ResponseCh <- l.ctx.Err():
		default:
		}
		return
	default:
	}

	logCount := uint64(len(logs))
	if logCount > 0 {
		if err := l.store.SaveLogs(l.ctx, request.Type, blockNumber, logs); err != nil {
			l.stats.StorageErrors++
			l.logger.Error("failed to process logs for "+request.Type.String(),
				zap.Uint64("block", blockNumber),
				zap.Error(err))
			return
		}
	}

	l.stats.updateLastReceivedBlock(request.Type, blockNumber)
	l.stats.increaseCounter(request.Type, logCount)

	select {
	case <-l.ctx.Done():
		select {
		case request.ResponseCh <- l.ctx.Err():
		default:
		}
		return
	default:
	}

	l.postProcess(request.Type, blockNumber)

	l.logger.Debug("processed "+request.Type.String(),
		zap.Uint64("block", blockNumber),
		zap.Uint64("log_count", logCount),
		zap.Duration("took", time.Since(start)))
}

func (l *LogAnalyzer) postProcess(logType LogType, blockNumber uint64) {
	if logType != FinalizedLogType {
		return
	}
	l.compareLogsForBlock(blockNumber)

	if err := l.store.DeleteLogs(l.ctx, blockNumber); err != nil {
		l.stats.StorageErrors++
		l.logger.Error("failed to delete temporary logs",
			zap.Uint64("block", blockNumber),
			zap.Error(err))
	}
	l.removeOldBlocks(blockNumber)
	l.generateReport()
}

func (l *LogAnalyzer) compareLogsForBlock(blockNumber uint64) {
	allLogs, err := l.store.GetAllLogs(l.ctx, blockNumber)
	if err != nil {
		l.stats.StorageErrors++
		l.logger.Error("failed to get logs for comparison",
			zap.Uint64("block", blockNumber),
			zap.Error(err))
		return
	}

	batchInternalLogs := allLogs.BatchLogs
	filterInternalLogs := allLogs.FilterLogs
	finalizedInternalLogs := allLogs.FinalizedLogs

	batchHashes := ExtractLogHashes(batchInternalLogs)
	filterHashes := ExtractLogHashes(filterInternalLogs)
	finalizedHashes := ExtractLogHashes(finalizedInternalLogs)

	missingInBatchCount, missingInFilterCount, extraInBatchCount, extraInFilterCount := CountDiscrepanciesByHash(
		batchHashes, filterHashes, finalizedHashes)

	stats := &BlockAnalysisStats{
		BlockNumber:       blockNumber,
		BatchLogCount:     uint64(len(allLogs.BatchLogs)),
		FilterLogCount:    uint64(len(allLogs.FilterLogs)),
		FinalizedLogCount: uint64(len(allLogs.FinalizedLogs)),
		AnalyzedAt:        time.Now().Unix(),
		IsFinal:           true,
		HasDiscrepancies:  missingInBatchCount > 0 || missingInFilterCount > 0 || extraInBatchCount > 0 || extraInFilterCount > 0,
		MissingInBatch:    missingInBatchCount,
		MissingInFilter:   missingInFilterCount,
		ExtraInBatch:      extraInBatchCount,
		ExtraInFilter:     extraInFilterCount,
	}

	if stats.HasDiscrepancies {
		l.logger.Warn("log discrepancies detected",
			zap.Uint64("block", blockNumber),
			zap.Uint64("missing_in_batch", missingInBatchCount),
			zap.Uint64("missing_in_filter", missingInFilterCount),
			zap.Uint64("extra_in_batch", extraInBatchCount),
			zap.Uint64("extra_in_filter", extraInFilterCount))
	}

	if err := l.store.SaveBlockStats(l.ctx, stats); err != nil {
		l.stats.StorageErrors++
		l.logger.Error("failed to save block stats",
			zap.Uint64("block", blockNumber),
			zap.Error(err))
	}

	if stats.HasDiscrepancies {
		problematicBlock := &ProblematicBlock{
			BlockNumber:        blockNumber,
			DetectedAt:         stats.AnalyzedAt,
			TotalDiscrepancies: missingInBatchCount + missingInFilterCount + extraInBatchCount + extraInFilterCount,
		}

		if err := l.store.SaveProblematicBlock(l.ctx, problematicBlock); err != nil {
			l.stats.StorageErrors++
			l.logger.Error("failed to save problematic block",
				zap.Uint64("block", blockNumber),
				zap.Error(err))
		} else {
			l.stats.ProblematicBlockCount++
			l.logger.Info("saved problematic block",
				zap.Uint64("block", blockNumber),
				zap.Uint64("total_discrepancies", problematicBlock.TotalDiscrepancies))
		}
	}
}

func (l *LogAnalyzer) removeOldBlocks(blockNumber uint64) {
	if l.storageConfig.RetainBlocks > 0 && blockNumber > l.storageConfig.RetainBlocks {
		oldBlock := blockNumber - l.storageConfig.RetainBlocks
		lastEvicted := l.stats.LastEvictedBlock
		if oldBlock > lastEvicted {
			if err := l.store.DeleteBlockData(l.ctx, oldBlock); err != nil {
				l.stats.StorageErrors++
				l.logger.Error("failed to delete old block data",
					zap.Uint64("block", oldBlock),
					zap.Error(err))
			} else {
				l.stats.LastEvictedBlock = oldBlock
				l.logger.Debug("evicted old block data", zap.Uint64("block", oldBlock))
			}
		}
	}
}

func (l *LogAnalyzer) generateReport() {
	// Calculate lag between head and finalized
	var lastReceived uint64
	for _, blockNum := range l.stats.LastReceivedBlock {
		if blockNum > lastReceived {
			lastReceived = blockNum
		}
	}
	lastFinalized := l.stats.LastReceivedBlock[FinalizedLogType]
	var headToFinalizedLag uint64
	if lastReceived >= lastFinalized {
		headToFinalizedLag = lastReceived - lastFinalized
	}

	batchQueue := l.logsQueues[BatchLogType]
	filterQueue := l.logsQueues[FilterLogType]
	finalizedQueue := l.logsQueues[FinalizedLogType]

	storageErrors := l.stats.StorageErrors
	problematicBlockCount := l.stats.ProblematicBlockCount

	// Warn if queue depths are getting high
	warningThreshold := int(QueueFullWarningThreshold * float64(l.maxQueueSize))
	for _, queue := range l.logsQueues {
		if depth := queue.depth(); depth > warningThreshold {
			l.logger.Warn(queue.logDescription+" queue depth is high",
				zap.Int("queue_depth", depth),
				zap.Uint64("dropped_total", queue.dropped()))
		}
	}

	l.logger.Debug("log analyzer stats",
		zap.Uint64("batch_log_count", l.stats.LogCount[BatchLogType]),
		zap.Uint64("filter_log_count", l.stats.LogCount[FilterLogType]),
		zap.Uint64("finalized_log_count", l.stats.LogCount[FinalizedLogType]),
		zap.Uint64("last_received_block", lastReceived),
		zap.Uint64("last_finalized_block", lastFinalized),
		zap.Uint64("last_evicted_block", l.stats.LastEvictedBlock),
		zap.Uint64("head_to_finalized_lag", headToFinalizedLag),
		zap.Int("batch_queue_depth", batchQueue.depth()),
		zap.Uint64("batch_queue_dropped", batchQueue.dropped()),
		zap.Int("filter_queue_depth", filterQueue.depth()),
		zap.Uint64("filter_queue_dropped", filterQueue.dropped()),
		zap.Int("finalized_queue_depth", finalizedQueue.depth()),
		zap.Uint64("finalized_queue_dropped", finalizedQueue.dropped()),
		zap.Uint64("storage_errors", storageErrors),
		zap.Uint64("problematic_block_count", problematicBlockCount),
		zap.Bool("storage_enabled", true),
		zap.Uint64("retention_blocks", l.storageConfig.RetainBlocks))
}
