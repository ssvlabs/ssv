// Package tbft is the SSV-side adapter that bridges the spec-independent
// TBFT consensus core (protocol/v2/tbft) with SSV's runtime — concrete
// types from ssv-spec, the network layer, the runner lifecycle.
//
// This package is the *only* place where SSV's TBFT integration depends
// on github.com/ssvlabs/ssv-spec. The TBFT core itself remains
// spec-independent.
package tbft

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
)

// Default protocol parameters. These can be overridden per-cluster via
// ConfigOverrides if a deployment wants to tune them.
//
// Values come from docs/TBFT.md and docs/TASKS.md (Phase 0 decisions):
//
//   - Deadline:    slot_start + 3s   (leaves headroom for the relay 4s cutoff)
//   - LateFetch:   slot_start + 2s   (typical TBFT; primary fetch in TBFT2)
//   - EarlyFetch:  slot_start − 4s   (TBFT2 backup fetch only)
const (
	DefaultDeadlineOffset   = 3 * time.Second
	DefaultLateFetchOffset  = 2 * time.Second
	DefaultEarlyFetchOffset = -4 * time.Second
)

// ConfigOverrides allows callers to override the default protocol timings.
// Zero values fall back to the package defaults.
type ConfigOverrides struct {
	DeadlineOffset   time.Duration
	LateFetchOffset  time.Duration
	EarlyFetchOffset time.Duration
}

func (o *ConfigOverrides) deadline() time.Duration {
	if o == nil || o.DeadlineOffset == 0 {
		return DefaultDeadlineOffset
	}
	return o.DeadlineOffset
}

func (o *ConfigOverrides) lateFetch() time.Duration {
	if o == nil || o.LateFetchOffset == 0 {
		return DefaultLateFetchOffset
	}
	return o.LateFetchOffset
}

func (o *ConfigOverrides) earlyFetch() time.Duration {
	if o == nil || o.EarlyFetchOffset == 0 {
		return DefaultEarlyFetchOffset
	}
	return o.EarlyFetchOffset
}

// ConfigForCluster builds a *tbft.Config for the given cluster + slot.
//
// The caller picks TBFT vs TBFT2 implicitly via the cluster size (see
// docs/TASKS.md):
//
//   - n=4 (f=1): TBFT2 — K=2 layers; layer 0 (primary) at late fetch
//     time, layer 1 (backup) at early fetch time. The primary leader
//     is index 0 in the per-slot rotation, the backup is index 1.
//   - n≥7      : TBFT  — K=max(3, f+1) layers, all at late fetch time.
//     Leaders are indices 0..K-1 in the per-slot rotation.
//
// Leader rotation matches SSV's existing QBFT round-robin convention
// (RoundRobinProposer): for height H, the layer-k leader is the
// committee member at index (H + k) mod n.
//
// `clusterID` should be a stable identifier for the cluster across slots
// (used in IBE tags to prevent cross-cluster replay). For SSV this is
// typically derived from the validator pubkey or the cluster's
// SSV-spec MessageID.
func ConfigForCluster(
	slot phase0.Slot,
	committee []spectypes.OperatorID,
	clusterID [32]byte,
	overrides *ConfigOverrides,
) (*tbftcore.Config, error) {
	if len(committee) == 0 {
		return nil, errors.New("tbft adapter: empty committee")
	}

	n := len(committee)
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("tbft adapter: cluster size %d is not 3f+1", n)
	}
	f := (n - 1) / 3

	// Sort committee by operator ID for deterministic rotation.
	sorted := make([]spectypes.OperatorID, len(committee))
	copy(sorted, committee)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Pick K based on cluster size.
	// TBFT2: K=2 (n=4 only); TBFT: K=max(3, f+1).
	K := 3
	if f+1 > K {
		K = f + 1
	}
	isTBFT2 := n == 4
	if isTBFT2 {
		K = 2
	}

	// Build leader rotation: layer k → committee[(slot + k) mod n].
	// This mirrors SSV's existing QBFT RoundRobinProposer rotation
	// convention so the same operator distribution applies.
	layers := make([]tbftcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		idx := (uint64(slot) + uint64(k)) % uint64(n)
		layers[k] = tbftcore.LayerSpec{
			Leader:  tbftcore.OperatorID(sorted[idx]),
			FetchAt: overrides.lateFetch(),
		}
	}
	// TBFT2 special-case: layer 1 (backup) fetches early.
	if isTBFT2 {
		layers[1].FetchAt = overrides.earlyFetch()
	}

	operators := make([]tbftcore.OperatorID, n)
	for i, op := range sorted {
		operators[i] = tbftcore.OperatorID(op)
	}

	return &tbftcore.Config{
		Height:    tbftcore.Height(slot),
		Layers:    layers,
		Deadline:  overrides.deadline(),
		ClusterID: clusterID,
		Operators: operators,
		F:         f,
	}, nil
}

// IsTBFT2 reports whether a config-built-by-ConfigForCluster is the
// 2-layer specialization (TBFT2) — true iff K == 2. Useful at runtime
// for deciding whether to schedule the early backup fetch.
func IsTBFT2(cfg *tbftcore.Config) bool {
	return cfg != nil && cfg.K() == 2
}
