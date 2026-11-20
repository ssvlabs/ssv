package executionclient

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
	"sort"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// LogID identifies a log within its block: its transaction's index and its block-level log index.
type LogID struct {
	TxIndex uint64
	Index   uint64
}

// BlockLogIDs returns the given logs' in-block identities, sorted and deduplicated so responses
// compare as sets. Because eth_getLogs can only ever return a subset of a block's logs — it drops,
// never invents — recorded IDs strictly contained in the receipts-derived ones prove the recorded
// response was incomplete. The background verifier uses this to detect logs a sync silently
// missed.
func BlockLogIDs(logs []ethtypes.Log) []LogID {
	ids := make([]LogID, len(logs))
	for i, l := range logs {
		ids[i] = LogID{TxIndex: uint64(l.TxIndex), Index: uint64(l.Index)}
	}
	slices.SortFunc(ids, func(a, b LogID) int {
		return cmp.Or(cmp.Compare(a.TxIndex, b.TxIndex), cmp.Compare(a.Index, b.Index))
	})
	return slices.Compact(ids)
}

// EncodeLogIDs serializes log IDs for the verification journal.
func EncodeLogIDs(ids []LogID) []byte {
	b := make([]byte, 0, 4*len(ids))
	for _, id := range ids {
		b = binary.AppendUvarint(b, id.TxIndex)
		b = binary.AppendUvarint(b, id.Index)
	}
	return b
}

var errMalformedLogIDs = errors.New("malformed log IDs")

// DecodeLogIDs parses the output of EncodeLogIDs.
func DecodeLogIDs(b []byte) ([]LogID, error) {
	var ids []LogID
	for len(b) > 0 {
		txIndex, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errMalformedLogIDs
		}
		b = b[n:]
		index, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errMalformedLogIDs
		}
		b = b[n:]
		ids = append(ids, LogID{TxIndex: txIndex, Index: index})
	}
	return ids, nil
}

// BlockLogs holds a block's number and its logs. Unsettled marks a block whose bloom matches the
// contract but whose logs couldn't be authoritatively established — the EL returned none and no
// client could serve its receipts — so it carries no logs and must be verified later.
type BlockLogs struct {
	BlockNumber uint64
	Logs        []ethtypes.Log
	Unsettled   bool
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

// packWithUnsettled is PackLogs plus a log-less Unsettled entry for each unsettled block, in block
// order.
func packWithUnsettled(logs []ethtypes.Log, unsettled []uint64) []BlockLogs {
	packed := PackLogs(logs)
	if len(unsettled) == 0 {
		return packed
	}
	for _, block := range unsettled {
		packed = append(packed, BlockLogs{BlockNumber: block, Unsettled: true})
	}
	slices.SortFunc(packed, func(a, b BlockLogs) int { return cmp.Compare(a.BlockNumber, b.BlockNumber) })
	return packed
}
