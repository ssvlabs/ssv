package executionclient

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

func TestPackLogs(t *testing.T) {
	// Empty input case
	logs := []types.Log{}
	result := PackLogs(logs)
	assert.Empty(t, result)

	// Single log case
	logs = []types.Log{
		{BlockNumber: 1, TxIndex: 0},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 1)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 1)
	assert.Equal(t, uint(0), result[0].Logs[0].TxIndex)

	// Multiple logs, same block
	logs = []types.Log{
		{BlockNumber: 2, TxIndex: 0},
		{BlockNumber: 2, TxIndex: 1},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 1)
	assert.Equal(t, uint64(2), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 2)
	assert.Equal(t, uint(0), result[0].Logs[0].TxIndex)
	assert.Equal(t, uint(1), result[0].Logs[1].TxIndex)

	// Multiple logs, different blocks
	logs = []types.Log{
		{BlockNumber: 1, TxIndex: 1},
		{BlockNumber: 2, TxIndex: 0},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 2)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Equal(t, uint(1), result[0].Logs[0].TxIndex)
	assert.Equal(t, uint64(2), result[1].BlockNumber)
	assert.Equal(t, uint(0), result[1].Logs[0].TxIndex)

	// Logs not sorted by block
	logs = []types.Log{
		{BlockNumber: 3, TxIndex: 1},
		{BlockNumber: 2, TxIndex: 0},
		{BlockNumber: 1, TxIndex: 1},
		{BlockNumber: 3, TxIndex: 0},
		{BlockNumber: 1, TxIndex: 0},
		{BlockNumber: 2, TxIndex: 1},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 3)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 2)
	assert.Equal(t, uint(0), result[0].Logs[0].TxIndex) // should be sorted
	assert.Equal(t, uint(1), result[0].Logs[1].TxIndex)
	assert.Equal(t, uint64(2), result[1].BlockNumber)
	assert.Len(t, result[1].Logs, 2)
	assert.Equal(t, uint(0), result[1].Logs[0].TxIndex)
	assert.Equal(t, uint(1), result[1].Logs[1].TxIndex)
	assert.Equal(t, uint64(3), result[2].BlockNumber)
	assert.Len(t, result[2].Logs, 2)
	assert.Equal(t, uint(0), result[2].Logs[0].TxIndex)
	assert.Equal(t, uint(1), result[2].Logs[1].TxIndex)

	// Logs not sorted by TxIndex
	logs = []types.Log{
		{BlockNumber: 1, TxIndex: 1},
		{BlockNumber: 1, TxIndex: 0},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 1)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 2)
	assert.Equal(t, uint(0), result[0].Logs[0].TxIndex) // should be sorted
	assert.Equal(t, uint(1), result[0].Logs[1].TxIndex)
}

func TestBlockLogsDigest(t *testing.T) {
	logs := []types.Log{
		{TxIndex: 0, Index: 0},
		{TxIndex: 0, Index: 1},
		{TxIndex: 2, Index: 5},
	}

	// Digesting is order-independent: it identifies logs by (TxIndex, Index), so the same set
	// in any order yields the same digest.
	shuffled := []types.Log{logs[2], logs[0], logs[1]}
	assert.Equal(t, BlockLogsDigest(logs), BlockLogsDigest(shuffled))

	// Dropping any log changes the digest — this is what lets the verifier detect a subset that
	// an incomplete eth_getLogs response returned.
	assert.NotEqual(t, BlockLogsDigest(logs), BlockLogsDigest(logs[:2]))

	// Only the (TxIndex, Index) identity matters; other log fields don't affect the digest.
	tagged := []types.Log{
		{TxIndex: 0, Index: 0, BlockNumber: 99, Removed: true},
		{TxIndex: 0, Index: 1},
		{TxIndex: 2, Index: 5},
	}
	assert.Equal(t, BlockLogsDigest(logs), BlockLogsDigest(tagged))

	// A different identity yields a different digest.
	moved := []types.Log{
		{TxIndex: 0, Index: 0},
		{TxIndex: 0, Index: 1},
		{TxIndex: 2, Index: 6}, // was Index 5
	}
	assert.NotEqual(t, BlockLogsDigest(logs), BlockLogsDigest(moved))

	// Empty/no logs is stable and distinct from any non-empty set.
	assert.Equal(t, BlockLogsDigest(nil), BlockLogsDigest([]types.Log{}))
	assert.NotEqual(t, BlockLogsDigest(nil), BlockLogsDigest(logs))
}
