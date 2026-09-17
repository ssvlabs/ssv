package dutytracer

import (
	"encoding/hex"
	"slices"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

func (c *Collector) dumpLinkToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var links = make(map[phase0.ValidatorIndex]spectypes.CommitteeID)

	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		committeeID, found := slotToCommittee.Get(slot)
		if !found {
			return true
		}

		// Remove from map to prevent new links from being added for this slot.
		slotToCommittee.Delete(slot)

		links[index] = committeeID

		return true
	})

	if err := c.store.SaveCommitteeDutyLinks(slot, links); err != nil {
		c.logger.Error("save validator to committee relations to disk", zap.Error(err))
		return
	}

	totalSaved = len(links)

	return
}

func (c *Collector) dumpCommitteeToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	dutiesByRole := make(map[spectypes.RunnerRole][]*traces.CommitteeDutyTrace)

	c.committeeTraces.Range(func(key committeeTraceKey, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		// Remove from map to prevent new messages from finding this trace.
		// Note: Concurrent operations that already hold a reference are protected by trace's lock below.
		slotToTraceMap.Delete(slot)

		// Now safely read the trace data while locked
		trace.Lock()

		pendingCount := 0
		for _, perSigner := range trace.pendingByRoot {
			for _, byTs := range perSigner {
				for _, idxs := range byTs {
					pendingCount += len(idxs)
				}
			}
		}
		if pendingCount > 0 {
			c.logger.Error("dropping buffered pending signatures during eviction (proposal never arrived or failed to establish role roots)",
				fields.Slot(slot), fields.CommitteeID(key.id),
				zap.Int("pending_entries", pendingCount),
				pendingDetails(trace.pendingByRoot))
			// We intentionally drop pending entries here; they are not persisted.
			trace.pendingByRoot = nil
		}

		// Deep copy while still holding the lock
		data := trace.DeepCopy()
		trace.Unlock()

		dutiesByRole[key.role] = append(dutiesByRole[key.role], data)
		return true
	})

	for role, duties := range dutiesByRole {
		if len(duties) == 0 {
			continue
		}
		// The traces are already out of memory, so whatever the store did not save is lost; count only
		// what it confirms. The error names an unencodable duty (the rest are on disk) or the failed batch.
		saved, err := c.store.SaveCommitteeDuties(slot, role, duties)
		if err != nil {
			c.logger.Error("couldn't save every committee duty to disk", zap.Error(err), fields.RunnerRole(role),
				zap.Int("saved", saved), zap.Int("lost", len(duties)-saved))
		}
		totalSaved += saved
	}

	return totalSaved
}

func (c *Collector) dumpValidatorToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var duties []*traces.ValidatorDutyTrace

	c.validatorTraces.Range(func(idx phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		// Remove from map to prevent new messages from finding this trace.
		slotToTraceMap.Delete(slot)

		// Now safely read the trace data
		for _, role := range trace.roleTraces() {
			if role.Validator == 0 {
				c.logger.Info("got trace with missing validator index", fields.ValidatorIndex(idx), fields.Slot(slot))
				role.Validator = idx
			}

			duties = append(duties, role)
		}

		return true
	})

	// The traces are already out of memory, so whatever the store did not save is lost; count only what it
	// confirms. The error names an unencodable duty (the rest are on disk) or the failed batch.
	saved, err := c.store.SaveValidatorDuties(duties)
	if err != nil {
		c.logger.Error("couldn't save every validator duty to disk", zap.Error(err),
			zap.Int("saved", saved), zap.Int("lost", len(duties)-saved))
	}

	return saved
}

// pendingDetails constructs a single zap field named "pending_signers_by_root"
// countPending returns the total number of pending validator-index entries
// held in a pendingByRoot map, across all roots, signers and timestamps.
func countPending(data map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex) int {
	count := 0
	for _, perSigner := range data {
		for _, byTs := range perSigner {
			for _, idxs := range byTs {
				count += len(idxs)
			}
		}
	}
	return count
}

// that logs the content of pendingByRoot in a JSON-friendly structure.
func pendingDetails(data map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex) zap.Field {
	out := make(map[string]map[string]any, len(data))
	for root, perSigner := range data {
		rhex := hex.EncodeToString(root[:])
		inner := make(map[string]any, len(perSigner))
		for signer, byTs := range perSigner {
			// Flatten across timestamps for log brevity and also include per-timestamp buckets
			union := make(map[uint64]struct{})
			// deterministic ts ordering
			tsKeys := make([]uint64, 0, len(byTs))
			for ts := range byTs {
				tsKeys = append(tsKeys, ts)
			}
			slices.Sort(tsKeys)

			buckets := make([]map[string]any, 0, len(tsKeys))
			for _, ts := range tsKeys {
				idxs := byTs[ts]
				if len(idxs) == 0 {
					continue
				}
				// dedup + sort per bucket
				ded := make(map[uint64]struct{}, len(idxs))
				for _, idx := range idxs {
					u := uint64(idx)
					ded[u] = struct{}{}
					union[u] = struct{}{}
				}
				arr := make([]uint64, 0, len(ded))
				for v := range ded {
					arr = append(arr, v)
				}
				slices.Sort(arr)
				buckets = append(buckets, map[string]any{"t": ts, "indices": arr})
			}
			// union indices sorted
			unionArr := make([]uint64, 0, len(union))
			for v := range union {
				unionArr = append(unionArr, v)
			}
			slices.Sort(unionArr)

			inner[strconv.FormatUint(signer, 10)] = map[string]any{
				"by_timestamp":  buckets,
				"union_indices": unionArr,
			}
		}
		out[rhex] = inner
	}
	return zap.Any("pending_signers_by_root", out)
}
