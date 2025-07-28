package loganalyzer

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// TestLogOptions allows customization of test log creation.
type TestLogOptions struct {
	BlockNumber uint64
	Index       uint
	Address     string
	TxHash      string
	Topics      []string
	Data        []byte
	Removed     bool
}

func testLogAnalyzer(t *testing.T) *LogAnalyzer {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := Config{Storage: StorageConfig{RetainBlocks: 1000}}
	analyzer := New(context.Background(), logger, nil, config)
	require.NotNil(t, analyzer)

	t.Cleanup(func() {
		<-analyzer.Close()
	})

	return analyzer
}

func testStore(t *testing.T) LogAnalyzerStore {
	t.Helper()
	logger := zaptest.NewLogger(t)
	db, err := badger.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return NewLogAnalyzerStore(db)
}

func testAPIHandler(t *testing.T) (*APIHandler, LogAnalyzerStore) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	analyzer := testLogAnalyzer(t)
	store := analyzer.GetStore()
	handler := NewAPIHandler(logger, store, analyzer)
	return handler, store
}

func createTestLog(opts TestLogOptions) ethtypes.Log {
	// Set defaults
	if opts.Address == "" {
		opts.Address = "0x1234567890123456789012345678901234567890"
	}
	if opts.TxHash == "" {
		opts.TxHash = "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	}
	if opts.Topics == nil {
		opts.Topics = []string{
			"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
		}
	}
	if opts.Data == nil {
		opts.Data = []byte("test data")
	}

	topics := make([]common.Hash, len(opts.Topics))
	for i, topic := range opts.Topics {
		topics[i] = common.HexToHash(topic)
	}

	return ethtypes.Log{
		Address:     common.HexToAddress(opts.Address),
		BlockNumber: opts.BlockNumber,
		TxHash:      common.HexToHash(opts.TxHash),
		Index:       opts.Index,
		Topics:      topics,
		Data:        opts.Data,
		Removed:     opts.Removed,
	}
}

func testLog(blockNumber uint64, index uint, address, txHash string) ethtypes.Log {
	return createTestLog(TestLogOptions{
		BlockNumber: blockNumber,
		Index:       index,
		Address:     address,
		TxHash:      txHash,
	})
}

func testInternalLog(blockNumber uint64, index uint, address, txHash string) InternalLog {
	ethLog := testLog(blockNumber, index, address, txHash)
	return ToInternalLog(ethLog)
}

func testBlockLogs(blockNumber uint64, numLogs int) executionclient.BlockLogs {
	logs := make([]ethtypes.Log, numLogs)
	for i := 0; i < numLogs; i++ {
		logs[i] = testLog(blockNumber, uint(i), "0x1234", "0xabcd")
	}
	return executionclient.BlockLogs{
		BlockNumber: blockNumber,
		Logs:        logs,
	}
}

func simpleTestLog(blockNumber uint64) ethtypes.Log {
	return createTestLog(TestLogOptions{BlockNumber: blockNumber})
}

func simpleTestInternalLog(blockNumber uint64) InternalLog {
	return ToInternalLog(simpleTestLog(blockNumber))
}

func simpleTestBlockLogs(blockNumber uint64) executionclient.BlockLogs {
	return executionclient.BlockLogs{
		BlockNumber: blockNumber,
		Logs:        []ethtypes.Log{simpleTestLog(blockNumber)},
	}
}
