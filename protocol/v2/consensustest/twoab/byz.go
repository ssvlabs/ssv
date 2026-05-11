package twoab

import (
	"crypto/sha256"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// internalByz is the 2ab-specific byz interface. Mirrors the bare OBFT
// adapter's internalByz, plus three Phase-2a hooks (AllowVerdictBroadcast /
// OverrideVerdict / BuildExtraVerdicts) for verdict-broadcast suppression
// and equivocation patterns.
//
// Each method has an honest default; concrete patterns selectively override.
type internalByz interface {
	// Phase 1
	LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan
	OverrideOwnPhase1Delay(s *sim, leader twoab.OperatorID) time.Duration

	// Phase 2a (verdict broadcast — new in 2ab)
	AllowVerdictBroadcast(op twoab.OperatorID, layer int) bool
	OverrideVerdict(s *sim, op twoab.OperatorID, layer int, v *twoab.Verdict) *twoab.Verdict
	BuildExtraVerdicts(s *sim, op twoab.OperatorID, layer int, v *twoab.Verdict) []*twoab.Verdict

	// Phase 2b (onion2b — replaces base's Commit)
	AllowOnion2bBroadcast(op twoab.OperatorID) bool
	OverrideOnion2b(s *sim, op twoab.OperatorID, o *twoab.Onion2b) *twoab.Onion2b
	BuildExtraOnion2bs(s *sim, op twoab.OperatorID, o *twoab.Onion2b) []*twoab.Onion2b
	OverrideOwnOnion2bDispatchDelay(s *sim, op twoab.OperatorID) time.Duration

	// Phase 3
	AllowCertificateBroadcast(op twoab.OperatorID) bool

	// Generic
	AllowDelivery(from, to twoab.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to twoab.OperatorID, kind ct.MsgKind) time.Duration
}

type broadcastPlan struct {
	V          twoab.Value
	Recipients []twoab.OperatorID // nil = all peers
}

type byzSet struct {
	Members []twoab.OperatorID
	Lookup  map[twoab.OperatorID]bool
}

func newByzSet(ops []ct.OperatorID) byzSet {
	s := byzSet{
		Members: make([]twoab.OperatorID, len(ops)),
		Lookup:  make(map[twoab.OperatorID]bool, len(ops)),
	}
	for i, op := range ops {
		s.Members[i] = twoab.OperatorID(op)
		s.Lookup[twoab.OperatorID(op)] = true
	}
	return s
}

func (s byzSet) Contains(op twoab.OperatorID) bool { return s.Lookup[op] }

// translateByz maps an abstract consensustest.ByzPattern to a 2ab-internal
// impl. Most catalog kinds translate faithfully; 2ab-specific extensions
// (verdict-equivocation / verdict-vs-action) are deferred to a follow-up
// Phase (the existing ByzKind enum doesn't yet name them; adding new
// abstract kinds requires touching every adapter and is split from this
// adapter-introduction commit).
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
		return byzMultiSilent{OnlyHonestLayer: k}, nil
	case ct.ByzEquivocate111:
		return byzEquivoc111{ByzSet: bs}, nil
	case ct.ByzEquivocateAllNR:
		return byzEquivocAllNR{ByzSet: bs}, nil
	case ct.ByzEquivocateSigmaLockedSplit:
		var recipientsA, recipientsB []twoab.OperatorID
		if len(p.Recipients) >= 2 {
			half := len(p.Recipients) / 2
			for _, r := range p.Recipients[:half] {
				recipientsA = append(recipientsA, twoab.OperatorID(r))
			}
			for _, r := range p.Recipients[half:] {
				recipientsB = append(recipientsB, twoab.OperatorID(r))
			}
		} else {
			recipientsA = []twoab.OperatorID{2}
			recipientsB = []twoab.OperatorID{3}
		}
		return byzEquivocSigmaLockedSplit{
			ByzSet:      bs,
			RecipientsA: recipientsA,
			RecipientsB: recipientsB,
		}, nil
	case ct.ByzHV1SelectiveDelivery:
		recipients := []twoab.OperatorID{}
		if len(p.Recipients) > 0 {
			for _, r := range p.Recipients {
				recipients = append(recipients, twoab.OperatorID(r))
			}
		} else {
			recipients = append(recipients, twoab.OperatorID(2))
		}
		return byzHV1Selective{
			ByzSet:     bs,
			Recipients: recipients,
		}, nil
	case ct.ByzFakeEncryptedPresence:
		garbageLayer := 1
		if p.Layer > 0 {
			garbageLayer = p.Layer
		}
		return byzFakeEncryptedPresence{
			ByzSet:       bs,
			SilentLayer:  0,
			GarbageLayer: garbageLayer,
		}, nil
	case ct.ByzSigmaRefusal:
		return byzSigmaRefusal{ByzSet: bs}, nil
	case ct.ByzWithholdLeader:
		return byzWithholdLeader{ByzSet: bs}, nil
	case ct.ByzCertWithholding:
		return byzCertWithholding{ByzSet: bs}, nil
	case ct.ByzCrossSigning:
		return byzCrossSigning{ByzSet: bs}, nil
	case ct.ByzFakePlaintextSigma:
		return byzFakePlaintextSigma{ByzSet: bs}, nil
	case ct.ByzCrossOnionEquivocation:
		return byzCrossOnionEquivocation{ByzSet: bs, Layer: p.Layer}, nil
	case ct.ByzLateLeaderBroadcast:
		return byzLateLeaderBroadcast{ByzSet: bs}, nil
	case ct.ByzPartialEquivocation:
		var recipientsA, recipientsB []twoab.OperatorID
		if len(p.Recipients) >= 2 {
			for _, r := range p.Recipients[:len(p.Recipients)-1] {
				recipientsA = append(recipientsA, twoab.OperatorID(r))
			}
			recipientsB = append(recipientsB, twoab.OperatorID(p.Recipients[len(p.Recipients)-1]))
		} else {
			recipientsA = []twoab.OperatorID{2, 3}
			recipientsB = []twoab.OperatorID{4}
		}
		return byzPartialEquivocation{
			ByzSet:      bs,
			RecipientsA: recipientsA,
			RecipientsB: recipientsB,
		}, nil
	case ct.ByzDelayedCommit:
		return byzDelayedOnion2b{ByzSet: bs}, nil
	case ct.ByzAggregatorBypass:
		return byzAggregatorBypass{ByzSet: bs}, nil
	case ct.ByzWitnessForgery:
		// 2ab has no Witnesses array (no Phase-1 σ_V exists). Rule-3
		// equivalent is exercised via ByzCrossOnionEquivocation. Surface
		// as ErrNotApplicable so the matrix skip-not-fail propagates.
		return nil, ct.ErrNotApplicable
	case ct.ByzGarbageMessages, ct.ByzExceedsRateLimit, ct.ByzOfflineDoubleVAttempt:
		// Reserved enum values — covered at other layers, not via the
		// scenario catalog.
		return nil, ct.ErrNotApplicable
	default:
		return nil, fmt.Errorf("twoab adapter: unknown ByzKind %v", p.Kind)
	}
}

// ---- honest defaults (mixin) -------------------------------------------

type honestDefaults struct{}

func (honestDefaults) AllowVerdictBroadcast(twoab.OperatorID, int) bool { return true }
func (honestDefaults) OverrideVerdict(_ *sim, _ twoab.OperatorID, _ int, v *twoab.Verdict) *twoab.Verdict {
	return v
}
func (honestDefaults) BuildExtraVerdicts(_ *sim, _ twoab.OperatorID, _ int, _ *twoab.Verdict) []*twoab.Verdict {
	return nil
}
func (honestDefaults) AllowOnion2bBroadcast(twoab.OperatorID) bool     { return true }
func (honestDefaults) AllowCertificateBroadcast(twoab.OperatorID) bool { return true }
func (honestDefaults) AllowDelivery(_, _ twoab.OperatorID, _ ct.MsgKind) bool {
	return true
}
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ twoab.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (honestDefaults) OverrideOnion2b(_ *sim, _ twoab.OperatorID, o *twoab.Onion2b) *twoab.Onion2b {
	return o
}
func (honestDefaults) BuildExtraOnion2bs(_ *sim, _ twoab.OperatorID, _ *twoab.Onion2b) []*twoab.Onion2b {
	return nil
}
func (honestDefaults) OverrideOwnPhase1Delay(_ *sim, _ twoab.OperatorID) time.Duration {
	return 0
}
func (honestDefaults) OverrideOwnOnion2bDispatchDelay(_ *sim, _ twoab.OperatorID) time.Duration {
	return 0
}

// ---- byzNone -----------------------------------------------------------

type byzNone struct{ honestDefaults }

func (byzNone) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// ---- byzSilentLeader ---------------------------------------------------

type byzSilentLeader struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSilentLeader) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzMultiSilent ----------------------------------------------------

type byzMultiSilent struct {
	honestDefaults
	OnlyHonestLayer int
}

func (b byzMultiSilent) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if layer < b.OnlyHonestLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzEquivoc111 -----------------------------------------------------

type byzEquivoc111 struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivoc111) LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	others := make([]twoab.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != leader {
			others = append(others, op)
		}
	}
	plans := make([]broadcastPlan, 0, len(others))
	for i, recipient := range others {
		v := append(twoab.Value{}, []byte{'b', 'y', 'z', '-', 'V', byte('1' + i)}...)
		plans = append(plans, broadcastPlan{V: v, Recipients: []twoab.OperatorID{recipient}})
	}
	return plans
}

// ---- byzEquivocAllNR ---------------------------------------------------

type byzEquivocAllNR struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivocAllNR) LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	all := s.operators
	return []broadcastPlan{
		{V: append(twoab.Value{}, "byz-V-A"...), Recipients: all},
		{V: append(twoab.Value{}, "byz-V-B"...), Recipients: all},
	}
}

// ---- byzEquivocSigmaLockedSplit ----------------------------------------

// σ-locked split equivocation: in 2ab, the same scenario that misses
// under bare OBFT (each receiver retains 1 V; σ-pool < qV; bare OBFT
// reaches no σ-quorum AND no NR-quorum at L_0 — slot miss) recovers via
// 2ab's NR-fall-through. Phase-2a verdict pool is split f-f across V_a /
// V_b (1-1 at f=1 / n=4); the byz leader self-observes both bundles and
// row-1 NRs in their own verdict; the silent rest also NR. Neither V
// reaches qV; row 5 → all-NR → NR-quorum at L_0 → advance to L_1 where
// the honest non-byz leader broadcasts in time → σ at L_1.
type byzEquivocSigmaLockedSplit struct {
	honestDefaults
	ByzSet      byzSet
	RecipientsA []twoab.OperatorID
	RecipientsB []twoab.OperatorID
}

func (b byzEquivocSigmaLockedSplit) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	vA := append(twoab.Value{}, "byz-V-A"...)
	vB := append(twoab.Value{}, "byz-V-B"...)
	return []broadcastPlan{
		{V: vA, Recipients: append([]twoab.OperatorID(nil), b.RecipientsA...)},
		{V: vB, Recipients: append([]twoab.OperatorID(nil), b.RecipientsB...)},
	}
}

// ---- byzPartialEquivocation -------------------------------------------

// Natural-recovery equivocation: byz leader at L_0 emits V_a to RecipientsA
// (size 2f) and V_b to RecipientsB (size 1). In bare OBFT the leader's
// own Phase-1 σ_V partial pushes V_a's σ-pool to 2f+1 = qV → SUCCESS at
// L_0 with V_a.
//
// In 2ab (Variant C — no Phase-1 σ_V):
//   - The byz leader self-observes BOTH bundles via the adapter's leader
//     self-observation path → 2 distinct V's retained at the leader →
//     row 1 NR verdict (equivocation observed).
//   - 2f recipients of V_a issue σV(V_a); the V_b recipient issues
//     σV(V_b). verdict_pool[V_a] = 2f < qV = 2f+1; verdict_pool[V_b] = 1
//     < qV. nr_pool = {leader} = 1 < qEnc.
//   - Row 5 → all NR → NR-quorum at L_0 → advance to L_1 → σ at L_1.
//
// 2ab pays one layer for removing the Phase-1 σ_V head-start; Pigeonhole
// 2 still holds (at most one V reaches qV cluster-wide).
type byzPartialEquivocation struct {
	honestDefaults
	ByzSet      byzSet
	RecipientsA []twoab.OperatorID
	RecipientsB []twoab.OperatorID
}

func (b byzPartialEquivocation) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	vA := append(twoab.Value{}, "byz-V-A"...)
	vB := append(twoab.Value{}, "byz-V-B"...)
	return []broadcastPlan{
		{V: vA, Recipients: append([]twoab.OperatorID(nil), b.RecipientsA...)},
		{V: vB, Recipients: append([]twoab.OperatorID(nil), b.RecipientsB...)},
	}
}

// ---- byzHV1Selective ---------------------------------------------------

type byzHV1Selective struct {
	honestDefaults
	ByzSet     byzSet
	Recipients []twoab.OperatorID
}

func (b byzHV1Selective) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	return []broadcastPlan{
		{V: honestV, Recipients: append([]twoab.OperatorID(nil), b.Recipients...)},
	}
}

// ---- byzFakeEncryptedPresence ------------------------------------------

type byzFakeEncryptedPresence struct {
	honestDefaults
	ByzSet       byzSet
	SilentLayer  int
	GarbageLayer int
}

func (b byzFakeEncryptedPresence) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) && layer == b.SilentLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakeEncryptedPresence) OverrideOnion2b(_ *sim, op twoab.OperatorID, o *twoab.Onion2b) *twoab.Onion2b {
	if !b.ByzSet.Contains(op) {
		return o
	}
	if b.GarbageLayer < 0 || b.GarbageLayer >= len(o.Layers) {
		return o
	}
	cp := cloneOnion2b(o)
	cp.Layers[b.GarbageLayer] = twoab.EncryptedLayer{
		Value:      append(twoab.Value{}, "byz-fake-V-at-deeper-layer"...),
		Ciphertext: []byte("garbage-bytes-not-a-valid-ibe-ciphertext"),
	}
	return cp
}

// ---- byzSigmaRefusal ---------------------------------------------------

// All byz never broadcast Onion2b (and thus contribute no σ partials).
type byzSigmaRefusal struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSigmaRefusal) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzSigmaRefusal) AllowOnion2bBroadcast(op twoab.OperatorID) bool {
	return !b.ByzSet.Contains(op)
}

// ---- byzWithholdLeader -------------------------------------------------

type byzWithholdLeader struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzWithholdLeader) LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) && layer == s.cfg.K-1 {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzCertWithholding ------------------------------------------------

type byzCertWithholding struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzCertWithholding) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzCertWithholding) AllowCertificateBroadcast(op twoab.OperatorID) bool {
	return !b.ByzSet.Contains(op)
}

// ---- byzCrossSigning (Rule 1) ------------------------------------------

// Byz silences their own leader-layer (naturally NR-emits there), then
// OverrideOnion2b injects a forged σ entry at the same layer. Rule 1 must
// target a layer < K-1 (no NR-tag at deepest layer).
type byzCrossSigning struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzCrossSigning) LeaderBroadcastPlan(_ *sim, leader twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

func (b byzCrossSigning) OverrideOnion2b(s *sim, op twoab.OperatorID, o *twoab.Onion2b) *twoab.Onion2b {
	if !b.ByzSet.Contains(op) {
		return o
	}
	leaderLayer := -1
	for k := 0; k < s.cfg.K; k++ {
		if s.operators[k%s.cfg.N] == op {
			leaderLayer = k
			break
		}
	}
	if leaderLayer < 0 || leaderLayer >= s.cfg.K-1 {
		return o
	}
	cp := cloneOnion2b(o)
	cp.Layers[leaderLayer] = twoab.EncryptedLayer{
		Value:      append(twoab.Value{}, s.canonValues[leaderLayer]...),
		Ciphertext: forgeSigmaPartialBytes(op, leaderLayer, s.canonValues[leaderLayer]),
	}
	return cp
}

// ---- byzFakePlaintextSigma (Rule 5) -----------------------------------

type byzFakePlaintextSigma struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzFakePlaintextSigma) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakePlaintextSigma) OverrideOnion2b(_ *sim, op twoab.OperatorID, o *twoab.Onion2b) *twoab.Onion2b {
	if !b.ByzSet.Contains(op) {
		return o
	}
	if len(o.Layers) == 0 {
		return o
	}
	cp := cloneOnion2b(o)
	cp.Layers[0] = twoab.EncryptedLayer{
		Value:      append(twoab.Value{}, "byz-fake-V-at-L_0"...),
		Ciphertext: forgeSigmaPartialBytes(op, 0, []byte("byz-fake-V-at-L_0")),
	}
	return cp
}

// ---- byzCrossOnionEquivocation (Rule 3) -------------------------------

type byzCrossOnionEquivocation struct {
	honestDefaults
	ByzSet byzSet
	Layer  int
}

func (b byzCrossOnionEquivocation) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzCrossOnionEquivocation) BuildExtraOnion2bs(s *sim, op twoab.OperatorID, o *twoab.Onion2b) []*twoab.Onion2b {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	if b.Layer < 0 || b.Layer >= len(o.Layers) {
		return nil
	}
	cp := cloneOnion2b(o)
	primeV := []byte("byz-V-prime")
	cp.Layers[b.Layer] = twoab.EncryptedLayer{
		Value:      append(twoab.Value{}, primeV...),
		Ciphertext: forgeSigmaPartialBytes(op, b.Layer, primeV),
	}
	return []*twoab.Onion2b{cp}
}

// ---- byzLateLeaderBroadcast --------------------------------------------

// Byz leader broadcasts so late that first-observation at honest receivers
// lands past T_commit. Honest receivers reject the bundle (ErrLatePhase1Bundle)
// — auth-only retention does not apply past T_commit. The cluster falls
// through to L_1 via NR-quorum.
type byzLateLeaderBroadcast struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzLateLeaderBroadcast) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzLateLeaderBroadcast) OverrideOwnPhase1Delay(s *sim, leader twoab.OperatorID) time.Duration {
	if !b.ByzSet.Contains(leader) {
		return 0
	}
	return 6 * s.cfg.BTT
}

// ---- byzDelayedOnion2b -------------------------------------------------

type byzDelayedOnion2b struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzDelayedOnion2b) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzDelayedOnion2b) OverrideOwnOnion2bDispatchDelay(s *sim, op twoab.OperatorID) time.Duration {
	if !b.ByzSet.Contains(op) {
		return 0
	}
	return 3 * s.cfg.BTT / 2 // mirror base's ByzDelayedCommit sizing
}

// ---- byzAggregatorBypass (negative test) -------------------------------

type byzAggregatorBypass struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzAggregatorBypass) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzAggregatorBypass) BuildExtraOnion2bs(s *sim, op twoab.OperatorID, o *twoab.Onion2b) []*twoab.Onion2b {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	primeV := []byte("byz-bypass-V-prime")
	forged := make([]*twoab.Onion2b, 0, len(s.operators)-1)
	for _, other := range s.operators {
		if other == op {
			continue
		}
		cp := cloneOnion2b(o)
		cp.OperatorID = other
		cp.Layers[0] = twoab.EncryptedLayer{
			Value:      append(twoab.Value{}, primeV...),
			Ciphertext: forgeSigmaPartialBytes(other, 0, primeV),
		}
		for k := 1; k < len(cp.Layers); k++ {
			cp.Layers[k] = twoab.EncryptedLayer{}
		}
		cp.NRPartials = nil
		forged = append(forged, cp)
	}
	return forged
}

// ---- helpers -----------------------------------------------------------

func clonePhase1Bundle(b *twoab.Phase1Bundle) *twoab.Phase1Bundle {
	cp := *b
	cp.Value = append(twoab.Value(nil), b.Value...)
	return &cp
}

func cloneVerdict(v *twoab.Verdict) *twoab.Verdict {
	cp := *v
	return &cp
}

func cloneOnion2b(o *twoab.Onion2b) *twoab.Onion2b {
	cp := &twoab.Onion2b{
		ClusterID:  o.ClusterID,
		OperatorID: o.OperatorID,
		Height:     o.Height,
		Layers:     make([]twoab.EncryptedLayer, len(o.Layers)),
		NRPartials: make([]twoab.NRPartial, len(o.NRPartials)),
	}
	for i, el := range o.Layers {
		cp.Layers[i] = twoab.EncryptedLayer{
			Value:      append(twoab.Value(nil), el.Value...),
			Ciphertext: append([]byte(nil), el.Ciphertext...),
		}
	}
	for i, nr := range o.NRPartials {
		cp.NRPartials[i] = twoab.NRPartial{
			Layer:      nr.Layer,
			PartialSig: append(twoab.Signature(nil), nr.PartialSig...),
		}
	}
	return cp
}

func cloneCertificate(c *twoab.Certificate) *twoab.Certificate {
	cp := *c
	cp.Value = append(twoab.Value(nil), c.Value...)
	cp.Signature = append(twoab.Signature(nil), c.Signature...)
	return &cp
}

// forgeSigmaPartialBytes returns deterministic bytes shaped like a real BLS
// partial signature, but not cryptographically valid. Mirrors the base
// adapter's helper; per-rule detection on the (op, layer, value) tuple
// fires on byte-distinct partials.
func forgeSigmaPartialBytes(op twoab.OperatorID, layer int, value []byte) []byte {
	h := sha256.Sum256(append(append([]byte{byte(op), byte(op >> 8), byte(layer), byte(layer >> 8)}, value...), 0xff))
	out := make([]byte, ct.StubSignatureSize)
	out[0] = 0xff
	for i := 1; i < len(out); i++ {
		out[i] = h[i%len(h)] ^ byte(i)
	}
	return out
}
