package faultnet

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/qa/faults"
)

// Forged-send budget. Plan stays pure and forges on every trigger; the decorator decides how many
// forged sets actually go out, because a value that REJECTs on the honest side costs the faulted
// operator gossipsub score, and an unthrottled one gets it graylisted (errata #139: role7-prefork
// sent 3009 forged messages in 13 minutes and operator 5 was cut off, so MSG-02's receiver half
// was never evidenced).
//
// Derivation from the node's own scoring parameters (network/topics/params):
//
//   - P4, invalid message deliveries, per topic: TopicWeight * w4 * n^2, with
//     w4 = graylistThreshold / (TopicWeight * maxInvalidMessagesAllowed^2) (topic_score.go:225).
//     TopicWeight cancels: P4 = -16000 * n^2 / 20^2 = -40 * n^2, where n is the count of REJECTed
//     messages this peer delivered on that topic (each direct peer counts separately; a REJECTed
//     message is not forwarded, so only direct peers count it).
//   - The positive side is capped at TopicScoreCap = 32.72 (peer_score.go:21).
//   - Thresholds (peer_score.go:15-17): gossip -4000, publish -8000, graylist -16000. Graylist is
//     reached at n = 20 on one topic. Below zero the peer is pruned from meshes; below the gossip
//     threshold it also loses gossip, which degrades what the faulted node RECEIVES.
//   - Decay: InvalidMessageDeliveriesDecay = 0.01^(1/100) = 0.955 per epoch (100-epoch decay,
//     topic_score.go:39), so a counter halves in about 15 epochs (96 minutes).
//
// So a run stays above the gossip threshold while n <= 9 on every topic (-40 * 81 = -3240).
// maxRejectsPerRun = 8 (-2560) leaves margin for other penalties. The worst case puts every forged
// message on one topic (all of the operator's validators in one committee, as on ssv-mini), so the
// cap is on the total, not per topic. A rate limit alone is not enough: at one 2-REJECT set per
// epoch, n converges to 2 / (1 - 0.955) = 44 and crosses graylist after about 13 epochs.
//
// Values run back to back share the counter (the peer ID persists across restarts, and RetainScore
// keeps a disconnected peer's score for 100 epochs). See the run-order note in qa/FAULTS.md §14.2.
const maxRejectsPerRun = 8

// Caps per value, in forged sets. A set is everything one Plan call forges.
const (
	// role7-prefork: 2 REJECTs per set (the role-7 and role-8 clones) -> 4 sets = 8 REJECTs.
	role7PreForkMaxSets = 4
	// auth-9-entries: 1 REJECT per set (the 9-entry packet) -> 3 sets = 3 REJECTs.
	// auth-mixed-signer: 2 REJECTs per set (mixed index, mixed signer) -> 3 sets = 6 REJECTs.
	// Addendum D.8/D.9: "cap forged REJECT packets to the first 3 slots".
	authRejectMaxSets = 3
)

// sendBudget is how often the decorator lets a fault's forged set through.
type sendBudget struct {
	// perEpoch allows at most one forged set per wall-clock epoch.
	perEpoch bool
	// maxSets caps the forged sets for the whole boot; 0 means no cap.
	maxSets int
}

// budgetFor returns the budget of a throttled fault, or false for an unthrottled one.
func budgetFor(f faults.Fault) (sendBudget, bool) {
	switch f {
	case faults.Role7PreFork:
		return sendBudget{perEpoch: true, maxSets: role7PreForkMaxSets}, true
	case faults.Auth9Entries, faults.AuthMixedSigner:
		return sendBudget{maxSets: authRejectMaxSets}, true
	case faults.VRPreForkSlotReplay:
		// IGNORE only, so no score cost; one clone per epoch keeps the count readable.
		return sendBudget{perEpoch: true}, true
	default:
		return sendBudget{}, false
	}
}

// gate is the decorator's mutable budget state. Its zero value is ready to use.
type gate struct {
	mu        sync.Mutex
	sets      int
	lastEpoch phase0.Epoch
	fired     bool
	announced bool
}

// allow reports whether a forged set may go out at wall-clock slot now, and records it if so.
func (g *gate) allow(b sendBudget, now phase0.Slot) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if b.maxSets > 0 && g.sets >= b.maxSets {
		return false
	}
	epoch := phase0.Epoch(now / slotsPerEpoch)
	if b.perEpoch && g.fired && epoch == g.lastEpoch {
		return false
	}
	g.sets++
	g.lastEpoch = epoch
	g.fired = true
	return true
}

// exhausted reports whether the total cap is spent.
func (g *gate) exhausted(b sendBudget) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return b.maxSets > 0 && g.sets >= b.maxSets
}

// announceExhausted returns true exactly once, the first time it is called.
func (g *gate) announceExhausted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.announced {
		return false
	}
	g.announced = true
	return true
}

// forges reports whether a plan sends anything beyond the honest message.
func forges(out []Outgoing) bool {
	for _, o := range out {
		if o.Resign {
			return true
		}
	}
	return false
}

// throttle applies f's budget to a plan. A plan that forges nothing passes through and does not
// consume budget. A suppressed plan is replaced by the honest message alone.
func (n *Network) throttle(f faults.Fault, msg *spectypes.SignedSSVMessage, slot, now phase0.Slot, out []Outgoing) []Outgoing {
	b, ok := budgetFor(f)
	if !ok || !forges(out) {
		return out
	}
	if n.gate.allow(b, now) {
		return out
	}
	if n.gate.exhausted(b) && n.gate.announceExhausted() {
		n.logger.Warn("🧪 qa fault: forged-send budget exhausted, sending honest messages only from now on",
			zap.String("qa_fault", string(f)),
			zap.Int("max_sets", b.maxSets))
	}
	return []Outgoing{{Msg: msg, Slot: slot}}
}
