package qbft

import (
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// internalByz is the QBFT-DES-internal byz interface for the real-instance
// wrapper. Honest operators run real qbft.Instance; byz operators don't
// instantiate one and the byz pattern fabricates messages from them
// directly.
//
// SuppressBroadcast lets byz patterns drop a message from an HONEST operator's
// broadcast — e.g., byzMultiSilent silences the first-K leaders' PROPOSEs
// even though those operators are running real Instances.
type internalByz interface {
	IsByz(op ct.OperatorID) bool
	ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan
	AllowDelivery(from, to ct.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to ct.OperatorID, kind ct.MsgKind) time.Duration
	SuppressBroadcast(from ct.OperatorID, kind ct.MsgKind, round int) bool
}

type proposalPlan struct {
	V          []byte
	Recipients []ct.OperatorID // nil = all peers
}

// translateByz maps the abstract ByzPattern to a QBFT-DES-internal impl.
// Returns ErrNotApplicable for OBFT-specific kinds that don't translate.
func translateByz(p ct.ByzPattern) (internalByz, error) {
	bs := newByzSet(p.ByzOperators)
	switch p.Kind {
	case ct.ByzNone:
		return byzNone{}, nil
	case ct.ByzSilentLeader:
		return byzSilentLeader{ByzSet: bs}, nil
	case ct.ByzMultiSilent:
		k := p.K
		if k <= 0 {
			k = 3
		}
		return byzMultiSilent{ByzSet: bs, SilentRounds: k}, nil
	case ct.ByzEquivocate111:
		return byzEquivoc111{ByzSet: bs}, nil
	case ct.ByzEquivocateAllNR:
		return byzEquivocAll{ByzSet: bs}, nil
	case ct.ByzEquivocateSigmaLockedSplit:
		return byzEquivocSplit{
			ByzSet:     bs,
			RecipientA: p.PickRecipient(0, 2),
			RecipientB: p.PickRecipient(1, 3),
		}, nil
	case ct.ByzHV1SelectiveDelivery, ct.ByzFakeEncryptedPresence:
		return nil, ct.ErrNotApplicable
	case ct.ByzSigmaRefusal:
		return byzSigmaRefusal{ByzSet: bs}, nil
	case ct.ByzPartialEquivocation:
		return byzPartialEquivocation{
			ByzSet:     bs,
			RecipientA: p.PickRecipient(0, 2),
			RecipientB: p.PickRecipient(1, 3),
			RecipientC: p.PickRecipient(2, 4),
		}, nil
	case ct.ByzCrossSigning, ct.ByzCrossOnionEquivocation, ct.ByzFakePlaintextSigma,
		ct.ByzLateLeaderBroadcast, ct.ByzWithholdLeader, ct.ByzAggregatorBypass,
		ct.ByzCertWithholding:
		// OBFT-only patterns; QBFT has no analog (no chained-onion encryption,
		// no per-layer leader-σ, no cluster-wide cert gossip).
		return nil, ct.ErrNotApplicable
	case ct.ByzGarbageMessages, ct.ByzExceedsRateLimit, ct.ByzOfflineDoubleVAttempt:
		// Reserved enum values — covered at other layers, not via the
		// scenario catalog. See ByzKind enum comments in
		// consensustest/byz.go for where each is actually exercised.
		return nil, ct.ErrNotApplicable
	default:
		return nil, fmt.Errorf("qbft adapter: unknown ByzKind %v", p.Kind)
	}
}

// byzSet is the set of byzantine operator IDs. Patterns iterate Members in
// insertion order for determinism; Lookup is for O(1) Contains.
type byzSet struct {
	Members []ct.OperatorID
	Lookup  map[ct.OperatorID]bool
}

func newByzSet(ops []ct.OperatorID) byzSet {
	s := byzSet{
		Members: append([]ct.OperatorID(nil), ops...),
		Lookup:  make(map[ct.OperatorID]bool, len(ops)),
	}
	for _, op := range ops {
		s.Lookup[op] = true
	}
	return s
}

func (s byzSet) Contains(op ct.OperatorID) bool { return s.Lookup[op] }

// ---- honest defaults (mixin) -------------------------------------------

type honestDefaults struct{}

func (honestDefaults) AllowDelivery(_, _ ct.OperatorID, _ ct.MsgKind) bool { return true }
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ ct.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (honestDefaults) SuppressBroadcast(_ ct.OperatorID, _ ct.MsgKind, _ int) bool { return false }

// ---- byzNone -----------------------------------------------------------

type byzNone struct{ honestDefaults }

func (byzNone) IsByz(_ ct.OperatorID) bool { return false }
func (byzNone) ProposalPlanForRound(_ *sim, _ ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	return []proposalPlan{{V: honestV}}
}

// ---- byzSilentLeader (any byz that's a leader stays silent) --------------

type byzSilentLeader struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSilentLeader) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzSilentLeader) ProposalPlanForRound(_ *sim, leader ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	if b.ByzSet.Contains(leader) {
		return nil // silent
	}
	return []proposalPlan{{V: honestV}}
}

// ---- byzMultiSilent (top SilentRounds rounds silent) -------------------

// All byz are silent at any round they lead, AND honest leaders are also
// silent for the first SilentRounds rounds (modeling "first K leaders silent").
type byzMultiSilent struct {
	honestDefaults
	ByzSet       byzSet
	SilentRounds int
}

func (b byzMultiSilent) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzMultiSilent) ProposalPlanForRound(_ *sim, _ ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if round <= b.SilentRounds {
		return nil
	}
	return []proposalPlan{{V: honestV}}
}

// SuppressBroadcast silences PROPOSEs from honest leaders for the first
// SilentRounds rounds. The Instance still constructs the PROPOSE; the
// network drops it before dispatch.
func (b byzMultiSilent) SuppressBroadcast(_ ct.OperatorID, kind ct.MsgKind, round int) bool {
	return kind == ct.KindLeaderBroadcast && round > 0 && round <= b.SilentRounds
}

// ---- byzEquivoc111 (1-1-1 split) ---------------------------------------

type byzEquivoc111 struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivoc111) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzEquivoc111) ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if !b.ByzSet.Contains(leader) || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	others := make([]ct.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		opCT := ct.OperatorID(op)
		if opCT != leader {
			others = append(others, opCT)
		}
	}
	plans := make([]proposalPlan, 0, len(others))
	for i, recipient := range others {
		v := []byte(fmt.Sprintf("byz-V-%d", i+1))
		plans = append(plans, proposalPlan{V: v, Recipients: []ct.OperatorID{recipient}})
	}
	return plans
}

// ---- byzEquivocAll (byz splits PROPOSE delivery 50/50) -----------------

// QBFT's analog of OBFT's "flood-both-V's-to-all" semantics. A literal flood
// to all wouldn't actually equivocate at QBFT receivers because
// AddFirstMsgForSignerAndRound silently dedups the second PROPOSE per
// (signer, round) — receivers would all register V_A (first arrived) and
// happily decide at R1. Splitting delivery 50/50 instead fragments the
// PREPARE pool across V_A/V_B, which times out R1 and triggers a round-
// change. The ByzKind name `ByzEquivocateAllNR` reflects the OBFT-side
// semantic (flood + NR fall-through); the QBFT translation produces the
// same outcome class (round-change recovery) via a different mechanism.
type byzEquivocAll struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivocAll) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzEquivocAll) ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if !b.ByzSet.Contains(leader) || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	others := make([]ct.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		opCT := ct.OperatorID(op)
		if opCT != leader {
			others = append(others, opCT)
		}
	}
	half := len(others) / 2
	// Independent copies — both Recipients otherwise share the `others`
	// backing array, which would alias if any future event handler mutated
	// the slice in place.
	return []proposalPlan{
		{V: []byte("byz-V-A"), Recipients: append([]ct.OperatorID(nil), others[:half]...)},
		{V: []byte("byz-V-B"), Recipients: append([]ct.OperatorID(nil), others[half:]...)},
	}
}

// ---- byzEquivocSplit (1-1 σ-locked split) -----------------------------

type byzEquivocSplit struct {
	honestDefaults
	ByzSet     byzSet
	RecipientA ct.OperatorID
	RecipientB ct.OperatorID
}

func (b byzEquivocSplit) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzEquivocSplit) ProposalPlanForRound(_ *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if !b.ByzSet.Contains(leader) || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	return []proposalPlan{
		{V: []byte("byz-V-A"), Recipients: []ct.OperatorID{b.RecipientA}},
		{V: []byte("byz-V-B"), Recipients: []ct.OperatorID{b.RecipientB}},
	}
}

// ---- byzPartialEquivocation (2-1 PROPOSE split) -----------------------

// QBFT analog of OBFT's PartialEquivocation. Byz leader sends PROPOSE(V_a)
// to {RecipientA, RecipientB} and PROPOSE(V_b) to {RecipientC}. PREPARE
// pool on V_a = 2 honest (the byz leader runs no real Instance, so no
// PREPARE from leader); pool on V_b = 1. Both < quorum (3 at n=4) → R1
// timeout → R2 with honest leader proposes fresh V → succeeds. Mirrors
// OBFT.md:477 BFT-comparison row "Byzantine leader equivocates, 2-1 split":
// QBFT recovers via fresh-V at R2, OBFT succeeds at L_0 via natural σ-quorum.
type byzPartialEquivocation struct {
	honestDefaults
	ByzSet     byzSet
	RecipientA ct.OperatorID
	RecipientB ct.OperatorID
	RecipientC ct.OperatorID
}

func (b byzPartialEquivocation) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzPartialEquivocation) ProposalPlanForRound(_ *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if !b.ByzSet.Contains(leader) || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	return []proposalPlan{
		{V: []byte("byz-V-A"), Recipients: []ct.OperatorID{b.RecipientA, b.RecipientB}},
		{V: []byte("byz-V-B"), Recipients: []ct.OperatorID{b.RecipientC}},
	}
}

// ---- byzSigmaRefusal (byz never PREPAREs/COMMITs) ---------------------

// Implemented by treating byz as silent at all rounds (no Instance, no
// messages). Functionally equivalent to byzSilentLeader but explicitly
// "never contributes anything".
type byzSigmaRefusal struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSigmaRefusal) IsByz(op ct.OperatorID) bool { return b.ByzSet.Contains(op) }
func (b byzSigmaRefusal) ProposalPlanForRound(_ *sim, leader ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	if b.ByzSet.Contains(leader) {
		return nil
	}
	return []proposalPlan{{V: honestV}}
}
