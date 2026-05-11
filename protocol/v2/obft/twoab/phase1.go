package twoab

import (
	"bytes"
	"fmt"
	"time"
)

// BuildPhase1Bundle produces the Phase-1 bundle for `layer` on `value`.
// The local operator must be the layer's leader; otherwise returns
// ErrNotLeader.
//
// Per spec §Phase 1 (Variant C), the bundle pairs the candidate value
// with the leader-auth signature at the envelope layer (not at this
// protocol layer — the SSV adapter wraps the returned bundle bytes in
// a SignedSSVMessage at the wire layer). No σ_V threshold partial is
// produced here; the leader emits σ at Phase 2b uniformly with all
// other operators.
//
// This method has no side effects beyond constructing the bundle (no
// EKM σ-lock, no retention update — bundles are self-observed via the
// regular ObservePhase1Bundle path after broadcast).
//
// Idempotent: calling with the same (layer, value) repeatedly returns
// equivalent bundles. Calling with the same `layer` but different
// `value` is permitted at the protocol layer (the caller's host-fetch
// loop is responsible for single-broadcast discipline); the protocol
// detects two distinct bundles from the same leader at receivers via
// Rule-2 evidence in ObservePhase1Bundle.
func (i *Instance) BuildPhase1Bundle(layer int, value Value) (*Phase1Bundle, error) {
	if i == nil {
		return nil, fmt.Errorf("twoab: nil instance")
	}
	if layer < 0 || layer >= i.cfg.K() {
		return nil, fmt.Errorf("twoab: %w: layer %d outside [0, %d)",
			ErrLayerOutOfRange, layer, i.cfg.K())
	}
	if i.cfg.Layers[layer].Leader != i.ownOperatorID {
		return nil, fmt.Errorf("twoab: %w: local operator %d is not leader at layer %d (leader is %d)",
			ErrNotLeader, i.ownOperatorID, layer, i.cfg.Layers[layer].Leader)
	}
	if len(value) == 0 {
		return nil, ErrEmptyValue
	}
	return &Phase1Bundle{
		ClusterID:  i.cfg.ClusterID,
		OperatorID: i.ownOperatorID,
		Height:     i.cfg.Height,
		Layer:      layer,
		Value:      append(Value{}, value...), // defensive copy
	}, nil
}

// ObservePhase1Bundle records a peer's (or the local operator's own,
// after fetch+broadcast) Phase-1 bundle. Per spec §Phase 1:
//
//   - Bundles must pass structural validation (cluster id, slot, layer
//     range, claimed leader matches the layer's designated leader,
//     non-empty value). Invalid bundles are returned with a validation
//     error; the caller's SSV adapter has already done the outer-
//     envelope auth check by this point.
//
//   - Observation time gates the retention mode:
//
//   - observedOffset ≤ T_accept_max: regular retention. The bundle
//     drives the operator's own Phase-2a verdict at this layer (Phase F).
//
//   - T_accept_max < observedOffset ≤ T_commit: auth-only retention.
//     The bundle's auth signature is retained for cross-verifying peer
//     re-flooded V claims, but does NOT drive the operator's own
//     verdict. Spec §Phase 1: "Auth-only retention does *not* allow
//     the operator to issue a KindVerdict based on this bundle (which
//     would re-open timing-fragmentation grief surfaces)."
//
//   - observedOffset > T_commit: rejected with ErrLatePhase1Bundle.
//     Phase 2a is over; retention is pointless.
//
//   - Up to 2 distinct value_roots are retained per (slot, layer,
//     leader_id), regardless of auth-only/regular split. Per spec
//     §Phase 1 / Retention bounds: "Further auth-valid bundles for the
//     same (slot, layer, leader_id) are dropped silently." The 2-distinct
//     cap is sufficient for Rule-2 leader-equivocation evidence; the
//     third bundle wouldn't add slashable information.
//
//   - Second distinct value_root → Rule 2 (leader equivocation)
//     evidence. The pair `(BundleA, BundleB)` is self-contained
//     slashable proof (both op-identity-signed at the envelope layer;
//     receivers MAY act on a single observed pair).
//
//   - Identical re-broadcasts (same Value, observed multiple times via
//     gossipsub mesh paths) are deduplicated and silently dropped after
//     the first retention. Re-broadcasts that promote auth-only ->
//     regular (a previously-auth-only bundle is observed again within
//     the accept window) DO upgrade the retention flag, since the first
//     auth-only observation didn't constitute a verdict-eligible event;
//     the later in-window observation does. (This codifies the spec's
//     intent that gossipsub re-flood within Phase 2a is the auth-only
//     escape hatch.)
func (i *Instance) ObservePhase1Bundle(b *Phase1Bundle, observedOffset time.Duration) error {
	if i == nil {
		return fmt.Errorf("twoab: nil instance")
	}
	if err := ValidatePhase1Bundle(b, i.cfg); err != nil {
		return err
	}
	if observedOffset > i.cfg.TCommit {
		return ErrLatePhase1Bundle
	}
	authOnly := observedOffset > i.cfg.TAcceptMax()

	if i.retainedBundles[b.Layer] == nil {
		i.retainedBundles[b.Layer] = make(map[OperatorID][]*retainedBundle)
	}
	retained := i.retainedBundles[b.Layer][b.OperatorID]

	// Dedup against already-retained value_roots. If the same V is seen
	// again with a "better" retention mode (auth-only → regular), promote.
	// Otherwise the second observation is a silent no-op.
	for _, r := range retained {
		if bytes.Equal(r.Bundle.Value, b.Value) {
			if r.AuthOnly && !authOnly {
				// Upgrade auth-only → regular retention.
				r.AuthOnly = false
				r.RetentionEstablishedAt = observedOffset
			}
			return nil
		}
	}

	// Distinct value_root.
	if len(retained) >= 2 {
		// Already have 2 distinct from this leader; drop the third+.
		return nil
	}

	copyB := deepCopyBundle(b)
	newEntry := &retainedBundle{
		Bundle:                 copyB,
		AuthOnly:               authOnly,
		RetentionEstablishedAt: observedOffset,
	}

	if len(retained) == 1 {
		// Second distinct → Rule 2 (leader equivocation).
		i.retainedBundles[b.Layer][b.OperatorID] = append(retained, newEntry)
		i.recordEvidence(Evidence{
			Rule:       EvidenceLeaderEquivocation,
			OperatorID: b.OperatorID,
			Layer:      b.Layer,
			LeaderEquivocation: &LeaderEquivocationEvidence{
				BundleA: retained[0].Bundle,
				BundleB: copyB,
			},
		})
	} else {
		// First retention.
		i.retainedBundles[b.Layer][b.OperatorID] = []*retainedBundle{newEntry}
	}
	return nil
}

// deepCopyBundle returns a defensive copy of b so retention state isn't
// affected by caller-owned slice mutation post-Observe.
func deepCopyBundle(b *Phase1Bundle) *Phase1Bundle {
	out := *b
	out.Value = append(Value{}, b.Value...)
	return &out
}
