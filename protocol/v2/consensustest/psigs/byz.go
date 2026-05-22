package psigs

import (
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// internalByz is the PSigs-side projection of the framework's ByzPattern,
// reduced to the three primitives the protocol can express:
//   - AllowSign(op): may `op` self-sign + broadcast?
//   - AllowDelivery(from, to, kind): may the per-pair broadcast land?
//   - OverrideDelay(rng, from, to, kind): per-pair delay override, -1 →
//     fall through to cfg.Network.
//   - OverrideOwnSignDispatchDelay(op): extra delay added on top of the
//     network delay for `op`'s OWN broadcast (used by ByzDelayedCommit
//     equivalent — late dispatch past the soft target).
type internalByz interface {
	AllowSign(op ct.OperatorID) bool
	AllowDelivery(from, to ct.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to ct.OperatorID, kind ct.MsgKind) time.Duration
	OverrideOwnSignDispatchDelay(op ct.OperatorID) time.Duration
}

// translateByz maps the framework's ByzPattern into a PSigs internalByz.
// PSigs is a single-broadcast threshold-collect protocol with no leader,
// no rounds, no encrypted onion — so most consensus-targeted byz patterns
// (equivocation, fake-encrypted-presence, cross-signing, etc.) have no
// PSigs-side analog. Those patterns return ErrNotApplicable so the
// catalog cell renders n/a.
//
// Supported patterns (per the OBFT-OPPORTUNISTIC-PHASE3 plan's minimal
// mapping):
//   - ByzNone — honest baseline.
//   - ByzSigmaRefusal — byz never signs (witnesses the cluster losing
//     one of its 2f+1 partial-sig sources).
//   - ByzDelayedCommit — byz signs and broadcasts on time but with extra
//     dispatch delay so the partial lands "late" relative to the cluster
//     equivalent of RoundEndOffset.
//
// All other ByzKind values return ErrNotApplicable.
//
// `btt` is the framework's BTT, used to scale the byzDelayed extra
// dispatch delay (matches the OBFT adapter's 1.5·BTT convention so the
// "delayed past the soft target" effect is BTT-proportional across
// stress sweep points).
func translateByz(p ct.ByzPattern, btt time.Duration) (internalByz, error) {
	switch p.Kind {
	case ct.ByzNone:
		return byzNone{}, nil
	case ct.ByzSigmaRefusal:
		return byzRefuse{ops: setOf(p.ByzOperators)}, nil
	case ct.ByzDelayedCommit:
		return byzDelayed{ops: setOf(p.ByzOperators), extra: 3 * btt / 2}, nil
	default:
		return nil, fmt.Errorf("%w: PSigs has no analog for ByzKind=%s",
			ct.ErrNotApplicable, p.Kind)
	}
}

func setOf(ids []ct.OperatorID) map[ct.OperatorID]bool {
	out := make(map[ct.OperatorID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// crashOverlay wraps an internalByz so a set of "crashed" operators are
// completely offline: they never sign/broadcast their partial (AllowSign)
// and receive nothing (AllowDelivery), so they can't reach the qV threshold
// and never decide. They're also absent from the mesh topology
// (MakeMeshTopology) and reported offline by sim.outcome(). Composes with
// any Kind; the crashed set is disjoint from the pattern's byz operators
// per Validate.
type crashOverlay struct {
	internalByz
	crashed map[ct.OperatorID]bool
}

func (c crashOverlay) AllowSign(op ct.OperatorID) bool {
	if c.crashed[op] {
		return false
	}
	return c.internalByz.AllowSign(op)
}

func (c crashOverlay) AllowDelivery(from, to ct.OperatorID, kind ct.MsgKind) bool {
	if c.crashed[from] || c.crashed[to] {
		return false
	}
	return c.internalByz.AllowDelivery(from, to, kind)
}

// ---- byzNone: all honest -----------------------------------------------

type byzNone struct{}

func (byzNone) AllowSign(_ ct.OperatorID) bool                      { return true }
func (byzNone) AllowDelivery(_, _ ct.OperatorID, _ ct.MsgKind) bool { return true }
func (byzNone) OverrideDelay(_ *mrand.Rand, _, _ ct.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (byzNone) OverrideOwnSignDispatchDelay(_ ct.OperatorID) time.Duration { return 0 }

// ---- byzRefuse: byz ops never sign -------------------------------------

type byzRefuse struct {
	ops map[ct.OperatorID]bool
}

func (b byzRefuse) AllowSign(op ct.OperatorID) bool                   { return !b.ops[op] }
func (byzRefuse) AllowDelivery(_, _ ct.OperatorID, _ ct.MsgKind) bool { return true }
func (byzRefuse) OverrideDelay(_ *mrand.Rand, _, _ ct.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (byzRefuse) OverrideOwnSignDispatchDelay(_ ct.OperatorID) time.Duration { return 0 }

// ---- byzDelayed: byz signs but with extra dispatch delay --------------

type byzDelayed struct {
	ops   map[ct.OperatorID]bool
	extra time.Duration
}

func (byzDelayed) AllowSign(_ ct.OperatorID) bool                      { return true }
func (byzDelayed) AllowDelivery(_, _ ct.OperatorID, _ ct.MsgKind) bool { return true }
func (byzDelayed) OverrideDelay(_ *mrand.Rand, _, _ ct.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (b byzDelayed) OverrideOwnSignDispatchDelay(op ct.OperatorID) time.Duration {
	if b.ops[op] {
		return b.extra
	}
	return 0
}
