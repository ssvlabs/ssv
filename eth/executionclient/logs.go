package executionclient

import (
	"sort"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// BlockLogs holds a block's number and its logs.
type BlockLogs struct {
	BlockNumber uint64
	Logs        []ethtypes.Log
}

// sortLogsCanonical sorts logs in place into canonical on-chain order: block number, then
// transaction index, then log index. The log-index tiebreaker is what keeps logs from the
// same transaction ordered — sort.Slice is not stable, and one transaction can emit several
// order-dependent events (e.g. bulkRegisterValidator emits one ValidatorAdded per validator,
// each bumping the owner's nonce); without it, same-tx logs can reorder and valid
// registrations get silently rejected on a nonce mismatch. It is the only log-ordering
// function in the package: route every raw-log sort through it (e.g. bloom recovery) so a
// second, divergent comparator can't creep back in.
func sortLogsCanonical(logs []ethtypes.Log) {
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		if logs[i].TxIndex != logs[j].TxIndex {
			return logs[i].TxIndex < logs[j].TxIndex
		}
		return logs[i].Index < logs[j].Index
	})
}

// PackLogs packs logs into []BlockLogs by their block number.
func PackLogs(logs []ethtypes.Log) []BlockLogs {
	sortLogsCanonical(logs)

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
