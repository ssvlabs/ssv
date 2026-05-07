package qbft

import (
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// internalByz is the QBFT-DES-internal byz interface. Each Allow* returns
// false to suppress the corresponding broadcast; ProposalPlanForRound may
// return multiple plans (equivocation) with optional per-plan Recipients
// (nil = broadcast to all). OverrideDelay returns -1 to defer to the
// network model.
type internalByz interface {
	LeaderProposesAtRound(leader ct.OperatorID, round int) bool
	ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan
	AllowPrepareBroadcast(op ct.OperatorID, round int) bool
	AllowCommitBroadcast(op ct.OperatorID, round int) bool
	AllowRoundChangeBroadcast(op ct.OperatorID, round int) bool
	AllowDelivery(from, to ct.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to ct.OperatorID, kind ct.MsgKind) time.Duration
}

type proposalPlan struct {
	V          []byte
	Recipients []ct.OperatorID // nil = all peers
}

// translateByz maps the abstract ByzPattern to a QBFT-DES-internal impl.
// Returns ErrNotApplicable for OBFT-specific kinds that don't translate.
func translateByz(p ct.ByzPattern) (internalByz, error) {
	switch p.Kind {
	case ct.ByzNone:
		return byzNone{}, nil
	case ct.ByzSilentLeader:
		return byzSilentLeader{Byz: p.PrimaryByz}, nil
	case ct.ByzMultiSilent:
		k := p.K
		if k <= 0 {
			k = 3
		}
		return byzMultiSilent{Byz: p.PrimaryByz, SilentRounds: k}, nil
	case ct.ByzEquivocate111:
		return byzEquivoc111{Byz: p.PrimaryByz}, nil
	case ct.ByzEquivocateAllNR:
		// QBFT semantics: byz proposer broadcasts conflicting PROPOSEs
		// to all honest. Receivers detect equivocation; PREPARE pool
		// fragments below qV; round 1 times out → R2.
		return byzEquivocAll{Byz: p.PrimaryByz}, nil
	case ct.ByzEquivocateSigmaLockedSplit:
		// Translation: byz proposer delivers V_a to subset, V_b to
		// other subset (equivalent to OBFT's σ-locked split). PREPARE
		// pool splits below qV → round 1 times out → R2.
		return byzEquivocSplit{
			Byz:        p.PrimaryByz,
			RecipientA: pickRecipient(p.Recipients, 0, 2),
			RecipientB: pickRecipient(p.Recipients, 1, 3),
		}, nil
	case ct.ByzHV1SelectiveDelivery, ct.ByzFakeEncryptedPresence:
		return nil, ct.ErrNotApplicable
	case ct.ByzSigmaRefusal:
		return byzSigmaRefusal{Byz: p.PrimaryByz}, nil
	default:
		return nil, fmt.Errorf("qbft adapter: unknown ByzKind %v", p.Kind)
	}
}

func pickRecipient(list []ct.OperatorID, idx int, fallback ct.OperatorID) ct.OperatorID {
	if idx < len(list) {
		return list[idx]
	}
	return fallback
}

// ---- honest defaults (mixin) -------------------------------------------

type honestDefaults struct{}

func (honestDefaults) LeaderProposesAtRound(_ ct.OperatorID, _ int) bool      { return true }
func (honestDefaults) AllowPrepareBroadcast(_ ct.OperatorID, _ int) bool      { return true }
func (honestDefaults) AllowCommitBroadcast(_ ct.OperatorID, _ int) bool       { return true }
func (honestDefaults) AllowRoundChangeBroadcast(_ ct.OperatorID, _ int) bool  { return true }
func (honestDefaults) AllowDelivery(_, _ ct.OperatorID, _ ct.MsgKind) bool    { return true }
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ ct.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}

// ---- byzNone -----------------------------------------------------------

type byzNone struct{ honestDefaults }

func (byzNone) ProposalPlanForRound(_ *sim, _ ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	return []proposalPlan{{V: honestV}}
}

// ---- byzSilentLeader (round-1 leader silent) ---------------------------

type byzSilentLeader struct {
	honestDefaults
	Byz ct.OperatorID
}

func (b byzSilentLeader) LeaderProposesAtRound(leader ct.OperatorID, round int) bool {
	if leader == b.Byz && round == 1 {
		return false
	}
	return true
}
func (byzSilentLeader) ProposalPlanForRound(_ *sim, _ ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	return []proposalPlan{{V: honestV}}
}

// ---- byzMultiSilent (top SilentRounds rounds silent) -------------------

// QBFT's analog of OBFT's K-1 silent leaders: we model `SilentRounds`
// consecutive proposers being byz/silent. Cluster round-changes through
// each until reaching an honest leader at round SilentRounds + 1.
type byzMultiSilent struct {
	honestDefaults
	Byz          ct.OperatorID
	SilentRounds int
}

func (b byzMultiSilent) LeaderProposesAtRound(_ ct.OperatorID, round int) bool {
	return round > b.SilentRounds
}
func (byzMultiSilent) ProposalPlanForRound(_ *sim, _ ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	return []proposalPlan{{V: honestV}}
}

// ---- byzEquivoc111 (1-1-1 split) ---------------------------------------

type byzEquivoc111 struct {
	honestDefaults
	Byz ct.OperatorID
}

func (b byzEquivoc111) ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if leader != b.Byz || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	others := make([]ct.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != b.Byz {
			others = append(others, op)
		}
	}
	plans := make([]proposalPlan, 0, len(others))
	for i, recipient := range others {
		v := []byte(fmt.Sprintf("byz-V-%d", i+1))
		plans = append(plans, proposalPlan{V: v, Recipients: []ct.OperatorID{b.Byz, recipient}})
	}
	return plans
}

// ---- byzEquivocAll (byz floods both V's to all honest) -----------------

type byzEquivocAll struct {
	honestDefaults
	Byz ct.OperatorID
}

func (b byzEquivocAll) ProposalPlanForRound(s *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if leader != b.Byz || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	// QBFT's "one PREPARE per round" rule means flooding both V's to all
	// receivers would just have everyone PREPARE on whichever V arrived first
	// (qV reachable on a single V → round 1 succeeds, contradicting the
	// scenario's PREPARE-fragmenting intent). Split delivery 50/50 instead.
	others := make([]ct.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != b.Byz {
			others = append(others, op)
		}
	}
	half := len(others) / 2
	groupA := append([]ct.OperatorID{b.Byz}, others[:half]...)
	groupB := append([]ct.OperatorID{b.Byz}, others[half:]...)
	return []proposalPlan{
		{V: []byte("byz-V-A"), Recipients: groupA},
		{V: []byte("byz-V-B"), Recipients: groupB},
	}
}

// ---- byzEquivocSplit (1-1 split) ---------------------------------------

type byzEquivocSplit struct {
	honestDefaults
	Byz        ct.OperatorID
	RecipientA ct.OperatorID
	RecipientB ct.OperatorID
}

func (b byzEquivocSplit) ProposalPlanForRound(_ *sim, leader ct.OperatorID, round int, honestV []byte) []proposalPlan {
	if leader != b.Byz || round != 1 {
		return []proposalPlan{{V: honestV}}
	}
	return []proposalPlan{
		{V: []byte("byz-V-A"), Recipients: []ct.OperatorID{b.Byz, b.RecipientA}},
		{V: []byte("byz-V-B"), Recipients: []ct.OperatorID{b.Byz, b.RecipientB}},
	}
}

// ---- byzSigmaRefusal (byz never prepares/commits) ----------------------

type byzSigmaRefusal struct {
	honestDefaults
	Byz ct.OperatorID
}

func (b byzSigmaRefusal) AllowPrepareBroadcast(op ct.OperatorID, _ int) bool {
	return op != b.Byz
}
func (b byzSigmaRefusal) AllowCommitBroadcast(op ct.OperatorID, _ int) bool {
	return op != b.Byz
}
func (byzSigmaRefusal) ProposalPlanForRound(_ *sim, _ ct.OperatorID, _ int, honestV []byte) []proposalPlan {
	return []proposalPlan{{V: honestV}}
}
