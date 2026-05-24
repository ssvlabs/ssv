package twoab

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Default protocol parameters per docs/2abOBFT.md §Setting — Config A
// (BTT = 200ms, RelayCutoff = 4000ms, HeaderSubmitHeadroom = 100ms).
//
// 2abOBFT has no T_commit hard wall (unlike bare OBFT): Phase 2a fires once at
// TPhase2a (early in the slot), Phase 2b is dynamic, and Phase 3 resolves
// opportunistically until the relay-submission cutoff. TPhase2a is positioned
// so the post-Phase-2a resolve window fits before the cutoff:
//
//	resolveBudget = BTT + max(SafetyBuffer, BTT) + ε_3 + JitterBuffer + HeaderSubmitHeadroom
//	TPhase2a      = RelayCutoff − resolveBudget
//	T_0_broadcast = TPhase2a − BTT     (V_0 gets 1·BTT to propagate before fire)
//
// The window reserves the MAX of the σ-path (1·BTT + SafetyBuffer) and the
// NR-fall-through path (2·BTT) — a slot resolves σ-ward XOR NR-ward, never
// both — i.e. BTT + max(SafetyBuffer, BTT). This mirrors the DES adapter
// derivation in protocol/v2/consensustest/twoab/adapter.go so the runner and
// the simulation produce consistent configs.
const (
	DefaultBTT                  = 200 * time.Millisecond
	DefaultRelayCutoff          = 4000 * time.Millisecond
	DefaultHeaderSubmitHeadroom = 100 * time.Millisecond
	DefaultEps3                 = 50 * time.Millisecond  // ε_3, absolute (local CPU reconstruction)
	DefaultJitterBuffer         = 50 * time.Millisecond  // residual jitter (Phase-3-complete → cert/submit)
	DefaultRefloodDelay         = 700 * time.Millisecond // default SafetyBuffer (cluster HeartbeatInterval)

	// DefaultK = f+1 = 2 at n=4 (BFT-liveness minimum), per docs/2abOBFT.md
	// §Application. Deployments preferring deeper fall-through can set K ∈
	// [f+2, n] via ConfigOverrides.K.
	DefaultK = 2
)

// ConfigOverrides lets callers override deployment-environment parameters and
// per-duty layer config. Zero values fall back to package defaults. Mirrors
// the bare-OBFT adapter's ConfigOverrides, swapping the timing model: 2abOBFT
// derives TPhase2a + SafetyBuffer (not TCommit + Delta2).
//
// Operator-facing surface: operators supply BTT (deployment P99 + δ) plus the
// deployment-environment values (RelayCutoff, HeaderSubmitHeadroom,
// RefloodDelay). All protocol timings derive deterministically so every
// operator in a cluster computes identical values.
type ConfigOverrides struct {
	K                    int
	BTT                  time.Duration // P99 + δ propagation+skew unit
	RelayCutoff          time.Duration // application hard deadline (e.g. 4s for proposer duty)
	HeaderSubmitHeadroom time.Duration // reserved for cert broadcast + relay submit (absolute)

	// RefloodDelay is the cluster's worst-case gossipsub reflood latency
	// (bounded by HeartbeatInterval). It is the default for SafetyBuffer.
	// Zero → DefaultRefloodDelay.
	RefloodDelay time.Duration

	// SafetyBuffer is the post-TPhase2a mesh-tail tolerance: the wall-clock
	// during which late peer KindValues propagate and σ-pool fills. Zero →
	// derived from RefloodDelay. Lowering it reclaims MEV-fetch headroom at
	// the cost of σ-pool-fill tolerance; see docs/2abOBFT.md §Timing.
	SafetyBuffer time.Duration

	// Test-only protocol-timing overrides. Production callers MUST NOT set
	// these — BTT is the only protocol-timing input. Unexported so external
	// callers cannot construct them; settable from package-internal fixtures.
	tPhase2aOverride     time.Duration
	eps3Override         time.Duration
	jitterBufferOverride time.Duration

	// FetchAt overrides the default per-layer fetch offsets. If nil, derived
	// as max(0, T_0_broadcast − B_k). Length must match K.
	FetchAt []time.Duration

	// BroadcastBudget overrides the default per-layer absorption windows B_k.
	// If nil, twoab.DefaultBroadcastBudget(K, BTT, T_0_broadcast) is used
	// (the staggered-shallow schedule B_k = (k+2)·BTT). Length must match K.
	BroadcastBudget []time.Duration
}

func (o *ConfigOverrides) k() int {
	if o == nil || o.K == 0 {
		return DefaultK
	}
	return o.K
}

func (o *ConfigOverrides) btt() time.Duration {
	if o == nil || o.BTT == 0 {
		return DefaultBTT
	}
	return o.BTT
}

func (o *ConfigOverrides) relayCutoff() time.Duration {
	if o == nil || o.RelayCutoff == 0 {
		return DefaultRelayCutoff
	}
	return o.RelayCutoff
}

func (o *ConfigOverrides) headerSubmitHeadroom() time.Duration {
	if o == nil || o.HeaderSubmitHeadroom == 0 {
		return DefaultHeaderSubmitHeadroom
	}
	return o.HeaderSubmitHeadroom
}

func (o *ConfigOverrides) refloodDelay() time.Duration {
	if o == nil || o.RefloodDelay == 0 {
		return DefaultRefloodDelay
	}
	return o.RefloodDelay
}

// safetyBuffer returns the explicit SafetyBuffer override if set, else the
// RefloodDelay-derived default (matching bare OBFT's structural budget).
func (o *ConfigOverrides) safetyBuffer() time.Duration {
	if o != nil && o.SafetyBuffer != 0 {
		return o.SafetyBuffer
	}
	return o.refloodDelay()
}

func (o *ConfigOverrides) eps3() time.Duration {
	if o == nil || o.eps3Override == 0 {
		return DefaultEps3
	}
	return o.eps3Override
}

func (o *ConfigOverrides) jitterBuffer() time.Duration {
	if o == nil || o.jitterBufferOverride == 0 {
		return DefaultJitterBuffer
	}
	return o.jitterBufferOverride
}

// resolveBudget reserves the post-TPhase2a window before RelayCutoff: the MAX
// of the σ-path and NR-fall-through path, plus ε_3 + jitter + header headroom.
func (o *ConfigOverrides) resolveBudget() time.Duration {
	btt := o.btt()
	return btt + max(o.safetyBuffer(), btt) + o.eps3() + o.jitterBuffer() + o.headerSubmitHeadroom()
}

// tPhase2a derives as RelayCutoff − resolveBudget. Test-only override via
// tPhase2aOverride.
func (o *ConfigOverrides) tPhase2a() time.Duration {
	if o != nil && o.tPhase2aOverride != 0 {
		return o.tPhase2aOverride
	}
	return o.relayCutoff() - o.resolveBudget()
}

// ConfigForCluster builds a *twoab.Config for the given cluster + slot.
//
// `clusterID` is a stable per-cluster identifier (NR-tag construction uses it
// to prevent cross-cluster replay); for SSV proposer duty it derives from the
// validator pubkey (SSVShare.CommitteeID()).
//
// Leader rotation: layer k → committee[(slot + k) mod n], matching the
// bare-OBFT adapter and SSV's QBFT RoundRobinProposer convention.
func ConfigForCluster(
	slot phase0.Slot,
	committee []spectypes.OperatorID,
	clusterID [32]byte,
	overrides *ConfigOverrides,
) (*twoabcore.Config, error) {
	if len(committee) == 0 {
		return nil, errors.New("twoab adapter: empty committee")
	}
	if overrides == nil {
		overrides = &ConfigOverrides{}
	}
	n := len(committee)
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("twoab adapter: cluster size %d is not 3f+1", n)
	}
	f := (n - 1) / 3

	K := overrides.k()
	minK := f + 1
	if K < minK {
		return nil, fmt.Errorf("twoab adapter: K=%d below BFT-liveness minimum %d (= f+1 at f=%d)",
			K, minK, f)
	}
	if K > n {
		return nil, fmt.Errorf("twoab adapter: K=%d exceeds cluster size %d", K, n)
	}

	btt := overrides.btt()
	tPhase2a := overrides.tPhase2a()
	// TPhase2a must exceed BTT so T_0_broadcast = TPhase2a − BTT lands within
	// the slot. At extreme operating points (BTT too large for the available
	// slot budget, or SafetyBuffer set too aggressively) the config is out of
	// envelope.
	if tPhase2a <= btt {
		return nil, fmt.Errorf(
			"twoab adapter: derived TPhase2a=%v <= BTT=%v (RelayCutoff=%v SafetyBuffer=%v): config out of envelope",
			tPhase2a, btt, overrides.relayCutoff(), overrides.safetyBuffer())
	}
	t0Broadcast := tPhase2a - btt

	sorted := make([]spectypes.OperatorID, len(committee))
	copy(sorted, committee)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	broadcastBudget := overrides.BroadcastBudget
	if broadcastBudget == nil {
		var err error
		broadcastBudget, err = twoabcore.DefaultBroadcastBudget(K, btt, t0Broadcast)
		if err != nil {
			return nil, fmt.Errorf("twoab adapter: %w", err)
		}
	}
	if len(broadcastBudget) != K {
		return nil, fmt.Errorf("twoab adapter: BroadcastBudget has %d entries, expected K=%d",
			len(broadcastBudget), K)
	}

	fetchAt := overrides.FetchAt
	if fetchAt == nil {
		// Apply the spec runtime clamp T_broadcast_max_k = max(0,
		// T_0_broadcast − B_k); each leader fetches by its broadcast deadline.
		fetchAt = make([]time.Duration, K)
		for k := 0; k < K; k++ {
			fa := t0Broadcast - broadcastBudget[k]
			if fa < 0 {
				fa = 0
			}
			fetchAt[k] = fa
		}
	}
	if len(fetchAt) != K {
		return nil, fmt.Errorf("twoab adapter: FetchAt has %d entries, expected K=%d", len(fetchAt), K)
	}

	layers := make([]twoabcore.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = twoabcore.LayerSpec{
			Leader:          leaderForLayer(sorted, twoabcore.Height(slot), k),
			FetchAt:         fetchAt[k],
			BroadcastBudget: broadcastBudget[k],
		}
	}

	operators := make([]twoabcore.OperatorID, n)
	for i, op := range sorted {
		operators[i] = twoabcore.OperatorID(op)
	}

	cfg := &twoabcore.Config{
		Height:       twoabcore.Height(slot),
		ClusterID:    clusterID,
		Operators:    operators,
		F:            f,
		Layers:       layers,
		TPhase2a:     tPhase2a,
		SafetyBuffer: overrides.safetyBuffer(),
		BTT:          btt,
	}
	return cfg, nil
}

// leaderForLayer maps (height, layer) → expected leader under the cluster's
// per-slot leader rotation. `sorted` is the cluster's operator IDs ascending.
func leaderForLayer(sorted []spectypes.OperatorID, height twoabcore.Height, layer int) twoabcore.OperatorID {
	n := uint64(len(sorted))
	if n == 0 {
		return 0
	}
	idx := (uint64(height) + uint64(layer)) % n //nolint:gosec // small positive ints
	return twoabcore.OperatorID(sorted[idx])
}
