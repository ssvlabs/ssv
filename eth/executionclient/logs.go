package executionclient

import (
	"sort"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// BlockLogs holds a block's number and it's logs.
type BlockLogs struct {
	BlockNumber uint64
	Logs        []ethtypes.Log
}

// PackLogs packs logs into []BlockLogs by their block number.
func PackLogs(logs []ethtypes.Log) []BlockLogs {
	// Sort into canonical on-chain order. The Index (logIndex) tiebreaker is what keeps logs
	// emitted by the same transaction in order: sort.Slice is not stable, and a single tx can
	// emit multiple order-dependent registry events (e.g. bulkRegisterValidator emits one
	// ValidatorAdded per validator, each bumping the owner's nonce), which the handler must
	// process in order. Without it, same-tx logs could be reordered and valid registrations
	// silently rejected on a nonce mismatch.
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		if logs[i].TxIndex != logs[j].TxIndex {
			return logs[i].TxIndex < logs[j].TxIndex
		}
		return logs[i].Index < logs[j].Index
	})

	var all []BlockLogs
	for _, log := range logs {
		// Create a BlockLogs if there isn't one for this block yet.
		if len(all) == 0 || all[len(all)-1].BlockNumber != log.BlockNumber {
			all = append(all, BlockLogs{
				BlockNumber: log.BlockNumber,
			})
		}

		// Append the log to the current BlockLogs.
		all[len(all)-1].Logs = append(all[len(all)-1].Logs, log)
	}

	return all
}
