package consensustest

import "time"

// RecordFirstOpportunisticQuorum captures the canonical "first σ-quorum
// reached via opportunistic Resolve" timestamp shared by the OBFT and
// 2abOBFT consensustest adapters. Each adapter maintains a per-op
// vQuorumAt map; this helper records `now + layer · epsilon3` for `op`
// if no entry exists yet, matching resolveOpAndBroadcastCert's decision-
// time formula (the ε_3 per-layer walk-cost reflects chained-decrypt +
// BLS aggregation per spec §Phase 3).
//
// Idempotent: returns false (no write) if `op` already has an entry. The
// first successful resolve wins; later resolves on richer pools recompute
// the same V (Pigeonhole 2) but a later wall-clock timestamp, which would
// inflate the recorded time-to-quorum past the actual moment σ-quorum
// first became locally reconstructible.
//
// Generic over the operator-id type so OBFT (obftbase.OperatorID) and
// 2abOBFT (twoab.OperatorID) can share it without runtime conversion;
// both are uint64 aliases under the parent obft package.
func RecordFirstOpportunisticQuorum[K comparable](
	vQuorumAt map[K]time.Duration,
	op K,
	layer int,
	now, epsilon3 time.Duration,
) bool {
	if _, exists := vQuorumAt[op]; exists {
		return false
	}
	vQuorumAt[op] = now + time.Duration(layer)*epsilon3
	return true
}
