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
		{BlockNumber: 1},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 1)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 1)

	// Multiple logs, same block
	logs = []types.Log{
		{BlockNumber: 2},
		{BlockNumber: 2},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 1)
	assert.Equal(t, uint64(2), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 2)

	// Multiple logs, different blocks
	logs = []types.Log{
		{BlockNumber: 1},
		{BlockNumber: 2},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 2)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 1)
	assert.Equal(t, uint64(2), result[1].BlockNumber)
	assert.Len(t, result[1].Logs, 1)

	// Logs not sorted by block
	logs = []types.Log{
		{BlockNumber: 3},
		{BlockNumber: 2},
		{BlockNumber: 1},
		{BlockNumber: 3},
		{BlockNumber: 1},
		{BlockNumber: 2},
	}
	result = PackLogs(logs)
	assert.Len(t, result, 3)
	assert.Equal(t, uint64(1), result[0].BlockNumber)
	assert.Len(t, result[0].Logs, 2)
	assert.Equal(t, uint64(2), result[1].BlockNumber)
	assert.Len(t, result[1].Logs, 2)
	assert.Equal(t, uint64(3), result[2].BlockNumber)
	assert.Len(t, result[2].Logs, 2)
}
