// Package obft is the SSV-side adapter that bridges the spec-independent
// OBFT consensus core (protocol/v2/obft) with SSV's runtime — concrete
// types from ssv-spec, the network layer, the runner lifecycle.
//
// This package is the only place where SSV's OBFT integration depends on
// github.com/ssvlabs/ssv-spec. The OBFT core itself remains spec-independent.
package obft

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Default protocol parameters per docs/OBFT.md §Timing budget — Config A
// (D = 100ms, δ = 50ms) with the recommended Δ_2 = 2(D + δ) = 300ms.
//
// The fetch-time spread comes from the asymmetric per-layer schedule:
// L_0 (primary) fetches latest at TCommit − 2(D+δ) − ε to capture late MEV;
// deeper layers fetch progressively earlier so backups build on
// deeper-confirmed parents.
const (
	DefaultD             = 100 * time.Millisecond
	DefaultDelta         = 50 * time.Millisecond
	DefaultTCommit       = 1500 * time.Millisecond
	DefaultDelta2        = 300 * time.Millisecond
	DefaultDelta3        = 250 * time.Millisecond
	DefaultPrimaryFetch  = 1100 * time.Millisecond // L_0 — late MEV
	DefaultBackupFetchK1 = 1050 * time.Millisecond // L_1
	DefaultBackupFetchK2 = 1000 * time.Millisecond // L_2
	DefaultBackupFetchK3 = 950 * time.Millisecond  // L_3 — earliest

	// DefaultK is the recommended layer count for SSV proposer duty:
	// K = n = 4 (every cluster member leads exactly one layer; max
	// fall-through depth at f = 1).
	DefaultK = 4

	// MinK is the minimum K OBFT supports per spec (late-leader resilience
	// requires K ≥ 3).
	MinK = 3
)

// ConfigOverrides allows callers to override the default protocol timings
// and layer count. Zero values fall back to package defaults.
type ConfigOverrides struct {
	K       int
	TCommit time.Duration
	Delta2  time.Duration
	Delta3  time.Duration
	D       time.Duration
	Delta   time.Duration

	// FetchAt overrides the default per-layer fetch offsets. If nil,
	// defaults are used (1100/1050/1000/950ms for K=4). Length must
	// match K (or zero/nil to use defaults).
	FetchAt []time.Duration
}

func (o *ConfigOverrides) k() int {
	if o == nil || o.K == 0 {
		return DefaultK
	}
	return o.K
}

func (o *ConfigOverrides) tCommit() time.Duration {
	if o == nil || o.TCommit == 0 {
		return DefaultTCommit
	}
	return o.TCommit
}

func (o *ConfigOverrides) delta2() time.Duration {
	if o == nil || o.Delta2 == 0 {
		return DefaultDelta2
	}
	return o.Delta2
}

func (o *ConfigOverrides) delta3() time.Duration {
	if o == nil || o.Delta3 == 0 {
		return DefaultDelta3
	}
	return o.Delta3
}

func (o *ConfigOverrides) d() time.Duration {
	if o == nil || o.D == 0 {
		return DefaultD
	}
	return o.D
}

func (o *ConfigOverrides) delta() time.Duration {
	if o == nil || o.Delta == 0 {
		return DefaultDelta
	}
	return o.Delta
}

// defaultFetchSchedule returns the K-tier per-layer FetchAt schedule —
// strictly decreasing in layer index k. K=2 isn't supported (MinK=3).
//
// For K=3 and K=4, hardcoded defaults are used (matching the spec's
// recommended per-layer fetch spread). For K>4 (n=7, n=10, n=13 deployments),
// fetches are interpolated linearly from primary (1100ms) to deepest (950ms).
func defaultFetchSchedule(K int) []time.Duration {
	switch K {
	case 3:
		return []time.Duration{
			DefaultPrimaryFetch,
			DefaultBackupFetchK1,
			DefaultBackupFetchK2,
		}
	case 4:
		return []time.Duration{
			DefaultPrimaryFetch,
			DefaultBackupFetchK1,
			DefaultBackupFetchK2,
			DefaultBackupFetchK3,
		}
	}
	// K > 4: linear interpolation across [DefaultBackupFetchK3, DefaultPrimaryFetch].
	out := make([]time.Duration, K)
	step := (DefaultPrimaryFetch - DefaultBackupFetchK3) / time.Duration(K-1)
	for k := 0; k < K; k++ {
		out[k] = DefaultPrimaryFetch - time.Duration(k)*step
	}
	return out
}

// ConfigForCluster builds an *obft.Config for the given cluster + slot.
//
// `clusterID` is a stable per-cluster identifier (used in NR-tag construction
// to prevent cross-cluster replay). For SSV proposer-duty, it's typically
// derived from the validator pubkey (`SSVShare.CommitteeID()`).
//
// Leader rotation: layer k → committee[(slot + k) mod n], mirroring SSV's
// QBFT RoundRobinProposer convention. At K = n, every operator leads
// exactly one layer per slot.
func ConfigForCluster(
	slot phase0.Slot,
	committee []spectypes.OperatorID,
	clusterID [32]byte,
	overrides *ConfigOverrides,
) (*obftcore.Config, error) {
	if len(committee) == 0 {
		return nil, errors.New("obft adapter: empty committee")
	}
	n := len(committee)
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("obft adapter: cluster size %d is not 3f+1", n)
	}
	f := (n - 1) / 3

	K := overrides.k()
	if K < MinK {
		return nil, fmt.Errorf("obft adapter: K=%d below minimum %d", K, MinK)
	}
	if K > n {
		return nil, fmt.Errorf("obft adapter: K=%d exceeds cluster size %d", K, n)
	}

	sorted := make([]spectypes.OperatorID, len(committee))
	copy(sorted, committee)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	fetchAt := overrides.FetchAt
	if fetchAt == nil {
		fetchAt = defaultFetchSchedule(K)
	}
	if len(fetchAt) != K {
		return nil, fmt.Errorf("obft adapter: FetchAt has %d entries, expected K=%d", len(fetchAt), K)
	}

	layers := make([]obftcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		idx := (uint64(slot) + uint64(k)) % uint64(n) //nolint:gosec // small positive ints
		layers[k] = obftcore.LayerSpec{
			Leader:  obftcore.OperatorID(sorted[idx]),
			FetchAt: fetchAt[k],
		}
	}

	operators := make([]obftcore.OperatorID, n)
	for i, op := range sorted {
		operators[i] = obftcore.OperatorID(op)
	}

	cfg := &obftcore.Config{
		Height:    obftcore.Height(slot),
		ClusterID: clusterID,
		Operators: operators,
		F:         f,
		Layers:    layers,
		TCommit:   overrides.tCommit(),
		Delta2:    overrides.delta2(),
		Delta3:    overrides.delta3(),
		D:         overrides.d(),
		Delta:     overrides.delta(),
	}
	return cfg, nil
}
