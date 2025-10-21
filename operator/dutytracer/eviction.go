package validator

import (
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
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

		links[index] = committeeID

		return true
	})

	if err := c.store.SaveCommitteeDutyLinks(slot, links); err != nil {
		c.logger.Error("save validator to committee relations to disk", zap.Error(err))
		return
	}

	c.validatorIndexToCommitteeLinks.Range(func(index phase0.ValidatorIndex, slotToCommittee *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		slotToCommittee.Delete(slot)
		return true
	})

	totalSaved = len(links)

	return
}

func (c *Collector) dumpCommitteeToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var duties []*exporter.CommitteeDutyTrace
	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}
		// Warn loudly if we are about to drop pending buffered entries due to roots not being resolved.
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
			c.logger.Error("discarding pending committee signer entries (roots unresolved)",
				fields.Slot(slot), fields.CommitteeID(key),
				zap.Int("pending_entries", pendingCount),
				pendingDetails(trace.pendingByRoot))
			// We intentionally drop pending entries here; they are not persisted.
			trace.pendingByRoot = nil
		}
		trace.Unlock()

		data := trace.trace()
		duties = append(duties, data)
		return true
	})

	if err := c.store.SaveCommitteeDuties(slot, duties); err != nil {
		c.logger.Error("save committee duties to disk", zap.Error(err))
		return
	}

	c.committeeTraces.Range(func(key spectypes.CommitteeID, slotToTraceMap *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		slotToTraceMap.Delete(slot)
		return true
	})

	totalSaved = len(duties)

	return
}

func (c *Collector) dumpValidatorToDBPeriodically(slot phase0.Slot) (totalSaved int) {
	var duties []*exporter.ValidatorDutyTrace
	c.validatorTraces.Range(func(idx phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		trace, found := slotToTraceMap.Get(slot)
		if !found {
			return true
		}

		for _, role := range trace.roleTraces() {
			if role.Validator == 0 {
				c.logger.Info("got trace with missing validator index", fields.ValidatorIndex(idx), fields.Slot(slot))
				role.Validator = idx
			}

			duties = append(duties, role)
		}

		return true
	})

	if err := c.store.SaveValidatorDuties(duties); err != nil {
		c.logger.Error("couldn't save validator duties to disk", zap.Error(err))
		return 0
	}

	c.validatorTraces.Range(func(_ phase0.ValidatorIndex, slotToTraceMap *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		slotToTraceMap.Delete(slot)
		return true
	})

	return len(duties)
}

// pendingDetails constructs a single zap field named "pending_signers_by_root"
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
			sort.Slice(tsKeys, func(i, j int) bool { return tsKeys[i] < tsKeys[j] })

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
				sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })
				buckets = append(buckets, map[string]any{"t": ts, "indices": arr})
			}
			// union indices sorted
			unionArr := make([]uint64, 0, len(union))
			for v := range union {
				unionArr = append(unionArr, v)
			}
			sort.Slice(unionArr, func(i, j int) bool { return unionArr[i] < unionArr[j] })

			inner[strconv.FormatUint(signer, 10)] = map[string]any{
				"by_timestamp":  buckets,
				"union_indices": unionArr,
			}
		}
		out[rhex] = inner
	}
	return zap.Any("pending_signers_by_root", out)
}
