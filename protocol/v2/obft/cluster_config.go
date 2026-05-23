package obft

import (
	"errors"
	"fmt"
	"time"
)

// LayerSpec describes one layer of the K-layer onion structure: the layer's
// leader, when (relative to slot start) it fetches its candidate value, and
// the per-layer broadcast-absorption budget B_k.
//
// Layers[0] is the primary L_0 (latest fetch — picks up MEV-late values);
// Layers[1..K-1] are progressively-deeper backups. The protocol that owns the
// Config anchors the broadcast deadline differently (bare OBFT on T_commit,
// 2abOBFT on T_0_broadcast); see each Config's BroadcastMaxOffsetForLayer.
type LayerSpec struct {
	Leader  OperatorID
	FetchAt time.Duration

	// BroadcastBudget is the layer's absorption target B_k: the leader aims to
	// broadcast its Phase-1 bundle by max(BFTStart, anchor − B_k). It is a
	// target, not a hard runtime cap. Required > 0 on every layer and must be
	// non-decreasing in layer index. Use each protocol's DefaultBroadcastBudget
	// for a spec-conforming schedule.
	BroadcastBudget time.Duration
}

// ValidateClusterTopology checks the cluster-membership and layer-topology
// invariants shared by both OBFT protocols: the byzantine bound, cluster
// size, layer count, operator/leader uniqueness, and the per-layer
// BroadcastBudget / FetchAt ordering.
//
// Each protocol's Config.Validate calls this, then validates its own timing
// fields and the anchor-dependent FetchAt ≤ broadcast-deadline bound (which
// differs by protocol: bare OBFT anchors on T_commit, 2abOBFT on
// T_0_broadcast).
func ValidateClusterTopology(operators []OperatorID, f int, layers []LayerSpec, btt time.Duration) error {
	if f < 1 {
		return errors.New("obft: byzantine bound F must be >= 1")
	}
	if len(operators) < 3*f+1 {
		return errors.New("obft: cluster size must be at least 3F+1")
	}
	// K ≥ f+1 is the BFT-liveness minimum (pigeonhole over the f-byz bound
	// guarantees ≥ 1 honest leader). K ≥ f+2 (late-leader-resilience) is a
	// deployment choice, not enforced here.
	minK := f + 1
	if len(layers) < minK {
		return fmt.Errorf("obft: K=%d below BFT-liveness minimum %d (= f+1 at f=%d)", len(layers), minK, f)
	}
	if len(layers) > len(operators) {
		return errors.New("obft: K cannot exceed cluster size")
	}
	if len(layers) > MaxLayers {
		return fmt.Errorf("obft: K=%d exceeds wire cap MaxLayers=%d (valid layer indices are [0, MaxLayers))", len(layers), MaxLayers)
	}
	if btt <= 0 {
		return errors.New("obft: BTT must be positive")
	}

	members := make(map[OperatorID]bool, len(operators))
	for _, op := range operators {
		if members[op] {
			return errors.New("obft: duplicate operator ID in cluster")
		}
		members[op] = true
	}

	for k, l := range layers {
		if l.BroadcastBudget <= 0 {
			return fmt.Errorf("obft: layer %d BroadcastBudget must be > 0 (use DefaultBroadcastBudget for a spec-conforming schedule)", k)
		}
	}
	// B_0 ≤ B_1 ≤ ... ≤ B_{K-1}: deeper layers get ≥ their predecessor's
	// absorption headroom. Equal adjacent budgets are tolerated (multiple
	// layers may clamp to BFTStart at degraded operating points).
	for k := 1; k < len(layers); k++ {
		if layers[k].BroadcastBudget < layers[k-1].BroadcastBudget {
			return errors.New("obft: BroadcastBudget must be non-decreasing in layer index (B_0 ≤ B_1 ≤ ...)")
		}
	}

	seenLeaders := make(map[OperatorID]bool, len(layers))
	for k, layer := range layers {
		if !members[layer.Leader] {
			return errors.New("obft: layer leader is not a cluster member")
		}
		if seenLeaders[layer.Leader] {
			return errors.New("obft: duplicate leader across layers")
		}
		seenLeaders[layer.Leader] = true
		if layer.FetchAt < 0 {
			return errors.New("obft: layer FetchAt must be non-negative")
		}
		// Deeper layers fetch ≤ their predecessor's offset (re-org resistance,
		// MEV-fetch asymmetry). Non-increasing (not strict-decreasing) lets
		// layers tie at BFTStart at degraded operating points.
		if k > 0 && layer.FetchAt > layers[k-1].FetchAt {
			return errors.New("obft: layer fetch times must be non-increasing in k")
		}
	}
	return nil
}
