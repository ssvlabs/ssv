package twoab

import (
	"crypto/sha256"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// internalByz is the 2ab-specific byz interface. Each method has an
// honest default; concrete patterns selectively override.
//
// Phase 2a now produces ONE of {ValueMsg, NoValueMsg, Commit-NRDirect}
// per op via MaybeFirePhase2a. We expose a coarse AllowPhase2aEmission
// gate (suppress all Phase-2a wire traffic from an op) plus per-message
// override / inject hooks for adversarial scenarios. The protocol-level
// "verdict-flip" attack (old σV ↔ NR) is obsolete: Phase 2a has no
// direction field — direction is derived from each op's local state.
//
// Phase 2b is now dynamic — Commits fire via the protocol's per-tick
// afterStateDelta cascade. The adapter captures these emissions via
// captureCascadeEmissions; OverrideCommit / BuildExtraCommits apply
// regardless of whether the commit fired at the Phase-2a-NRDirect
// fire-instant or via the cascade.
//
// Cross-kind extras (BuildExtraValueMsgs / BuildExtraNoValueMsgs /
// BuildExtraCommits): events.go calls all three hooks UNCONDITIONALLY
// at the Phase-2a-fire instant, passing the natural-kind emission for
// the matching hook and nil for the other two. Honest defaults return
// nil regardless. Byz patterns that want to inject MORE of the same
// kind do so on the matching hook; patterns that want to inject a
// DIFFERENT kind from the natural emission (e.g., natural KindValue +
// extra KindNoValue for Rule-6a downgrade testing) gate on the natural-
// kind parameter being nil (or non-nil for same-kind extras). MUST
// nil-guard the natural-emission parameter — events.go passes nil for
// kinds the natural path didn't fire.
//
// At the cascade path (captureCascadeEmissions), only BuildExtraCommits
// fires; cascade-driven upgrade-ValueMsgs go through
// OverrideUpgradeValueMsg only, no extras. This asymmetry is
// intentional — the cascade fires at most once per op per kind, and
// per-fire-instant equivocation is sufficient for the Rule-6a-style
// scenarios catalog needs.
type internalByz interface {
	// Phase 1
	LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan
	OverrideOwnPhase1Delay(s *sim, leader twoab.OperatorID) time.Duration

	// Phase 2a (coordination broadcast — new in 2abOBFT)
	AllowPhase2aEmission(op twoab.OperatorID) bool
	OverrideValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg
	OverrideUpgradeValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg
	OverrideNoValueMsg(s *sim, op twoab.OperatorID, nv *twoab.NoValueMsg) *twoab.NoValueMsg
	BuildExtraValueMsgs(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) []*twoab.ValueMsg
	BuildExtraNoValueMsgs(s *sim, op twoab.OperatorID, nv *twoab.NoValueMsg) []*twoab.NoValueMsg

	// Phase 2b (binding commit emission — dynamic in 2abOBFT)
	OverrideCommit(s *sim, op twoab.OperatorID, c *twoab.Commit) *twoab.Commit
	BuildExtraCommits(s *sim, op twoab.OperatorID, c *twoab.Commit) []*twoab.Commit
	OverrideOwnCommitDispatchDelay(s *sim, op twoab.OperatorID) time.Duration

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

// crashOverlay wraps an internalByz so a set of "crashed" operators are
// completely offline: no Phase-1 leader broadcast, no Phase-2a KindValue/
// KindNoValue, no Phase-2b commit, no certificate, and no wire delivery in
// or out. Layered on top of whatever byz Kind the scenario selected (the
// crashed set is disjoint from the pattern's byz operators per Validate).
// Phase-2b commits have no dedicated Allow gate, so they're suppressed at
// the wire (AllowDelivery for from=crashed) plus the s.crashed guard in
// emitMesh/emitDirect (which also avoids the NodeForOperator panic on a
// crashed publisher absent from the mesh topology). Decision bookkeeping —
// excluding crashed ops and stamping Err="offline" — lives in sim.outcome().
type crashOverlay struct {
	internalByz
	crashed byzSet
}

func (c crashOverlay) LeaderBroadcastPlan(s *sim, leader twoab.OperatorID, layer int, honestV twoab.Value) []broadcastPlan {
	if c.crashed.Contains(leader) {
		return nil
	}
	return c.internalByz.LeaderBroadcastPlan(s, leader, layer, honestV)
}

func (c crashOverlay) AllowPhase2aEmission(op twoab.OperatorID) bool {
	if c.crashed.Contains(op) {
		return false
	}
	return c.internalByz.AllowPhase2aEmission(op)
}

func (c crashOverlay) AllowCertificateBroadcast(op twoab.OperatorID) bool {
	if c.crashed.Contains(op) {
		return false
	}
	return c.internalByz.AllowCertificateBroadcast(op)
}

func (c crashOverlay) AllowDelivery(from, to twoab.OperatorID, kind ct.MsgKind) bool {
	if c.crashed.Contains(from) || c.crashed.Contains(to) {
		return false
	}
	return c.internalByz.AllowDelivery(from, to, kind)
}

// translateByz maps an abstract consensustest.ByzPattern to a 2ab-internal
// impl. Most catalog kinds translate faithfully; old verdict-flip /
// verdict-withhold patterns are gone (Phase 2a has no direction field
// to flip), and 2ab-specific extensions are deferred.
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
		return byzDelayedCommit{ByzSet: bs}, nil
	case ct.ByzPhase2EquivocateCrossV:
		return byzPhase2EquivocateCrossV{ByzSet: bs}, nil
	case ct.ByzPhase2DowngradeValueNoValue:
		return byzPhase2DowngradeValueNoValue{ByzSet: bs}, nil
	case ct.ByzAggregatorBypass:
		return byzAggregatorBypass{ByzSet: bs}, nil
	case ct.ByzWitnessForgery:
		// 2abOBFT has no Witnesses array — the Phase-1 σ_V leader partial
		// was removed entirely. Rule-3 equivalent is exercised via
		// ByzCrossOnionEquivocation. Surface as ErrNotApplicable so the
		// matrix skip-not-fail propagates.
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

func (honestDefaults) AllowPhase2aEmission(twoab.OperatorID) bool { return true }
func (honestDefaults) OverrideValueMsg(_ *sim, _ twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	return v
}
func (honestDefaults) OverrideUpgradeValueMsg(_ *sim, _ twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	return v
}
func (honestDefaults) OverrideNoValueMsg(_ *sim, _ twoab.OperatorID, nv *twoab.NoValueMsg) *twoab.NoValueMsg {
	return nv
}
func (honestDefaults) BuildExtraValueMsgs(_ *sim, _ twoab.OperatorID, _ *twoab.ValueMsg) []*twoab.ValueMsg {
	return nil
}
func (honestDefaults) BuildExtraNoValueMsgs(_ *sim, _ twoab.OperatorID, _ *twoab.NoValueMsg) []*twoab.NoValueMsg {
	return nil
}
func (honestDefaults) OverrideCommit(_ *sim, _ twoab.OperatorID, c *twoab.Commit) *twoab.Commit {
	return c
}
func (honestDefaults) BuildExtraCommits(_ *sim, _ twoab.OperatorID, _ *twoab.Commit) []*twoab.Commit {
	return nil
}
func (honestDefaults) AllowCertificateBroadcast(twoab.OperatorID) bool { return true }
func (honestDefaults) AllowDelivery(_, _ twoab.OperatorID, _ ct.MsgKind) bool {
	return true
}
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ twoab.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (honestDefaults) OverrideOwnPhase1Delay(_ *sim, _ twoab.OperatorID) time.Duration {
	return 0
}
func (honestDefaults) OverrideOwnCommitDispatchDelay(_ *sim, _ twoab.OperatorID) time.Duration {
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

// σ-locked split equivocation: in 2abOBFT, the same scenario that misses
// under bare OBFT (each receiver retains 1 V; σ-pool < qV; bare OBFT
// reaches no σ-quorum AND no NR-quorum at L_0 — slot miss) recovers via
// 2abOBFT's NR-fallthrough. The Phase-2a coordination broadcast surfaces
// the cluster's σ-eligibility verdict before any cryptographic commit
// fires; with f-f split, neither V reaches qV at value_pool, but
// noValuePool covers the rest of the cluster and NR-quorum unlocks L_1.
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
// (size 2f) and V_b to RecipientsB (size 1). In 2abOBFT:
//   - The byz leader self-observes BOTH bundles via the adapter's leader
//     self-observation path → 2 distinct V's retained at the leader →
//     Phase-2a NRDirect (equivocation observed).
//   - 2f recipients of V_a issue ValueMsg(V_a); the V_b recipient issues
//     ValueMsg(V_b). value_pool[V_a] = 2f < qV = 2f+1; value_pool[V_b] = 1
//     < qV. noValue_pool = {leader's NRDirect} = 1 < qEnc.
//   - At cluster σ-eligibility check: no V reaches qV; NR-eligibility
//     across the cluster's noValuePool entries advances to L_1; honest
//     L_1 leader's bundle propagates on time → σ at L_1.
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

// Byz silences at SilentLayer (L_0) and injects garbage into the L_k>0
// LayerEntry at GarbageLayer. Rule 4 fires at receivers when Phase 3's
// chain-decryption walk decrypts the entry into non-verifying bytes.
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

// patchLayerEntries replaces the entry at GarbageLayer with a forged
// SigmaChained entry. Returns a new slice (defensive copy).
func (b byzFakeEncryptedPresence) patchLayerEntries(op twoab.OperatorID, entries []twoab.LayerEntry) []twoab.LayerEntry {
	out := make([]twoab.LayerEntry, len(entries))
	for i, e := range entries {
		out[i] = twoab.LayerEntry{
			Layer:   e.Layer,
			Kind:    e.Kind,
			V:       append(twoab.Value(nil), e.V...),
			Payload: append([]byte(nil), e.Payload...),
		}
	}
	for i, e := range out {
		if e.Layer != b.GarbageLayer {
			continue
		}
		out[i] = twoab.LayerEntry{
			Layer:   b.GarbageLayer,
			Kind:    twoab.LayerEntrySigmaChained,
			V:       append(twoab.Value{}, "byz-fake-V-at-deeper-layer"...),
			Payload: forgeSigmaPartialBytes(op, b.GarbageLayer, []byte("byz-fake-V-at-deeper-layer")),
		}
		break
	}
	return out
}

func (b byzFakeEncryptedPresence) OverrideValueMsg(_ *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	if !b.ByzSet.Contains(op) {
		return v
	}
	cp := cloneValueMsg(v)
	cp.LayerEntries = b.patchLayerEntries(op, cp.LayerEntries)
	return cp
}

func (b byzFakeEncryptedPresence) OverrideUpgradeValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	return b.OverrideValueMsg(s, op, v)
}

func (b byzFakeEncryptedPresence) OverrideNoValueMsg(_ *sim, op twoab.OperatorID, nv *twoab.NoValueMsg) *twoab.NoValueMsg {
	if !b.ByzSet.Contains(op) {
		return nv
	}
	cp := cloneNoValueMsg(nv)
	cp.LayerEntries = b.patchLayerEntries(op, cp.LayerEntries)
	return cp
}

func (b byzFakeEncryptedPresence) OverrideCommit(_ *sim, op twoab.OperatorID, c *twoab.Commit) *twoab.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	if c.Side != twoab.CommitSideNRDirect {
		// L_k>0 commitments live in Phase-2a LayerEntries, not in
		// Phase-2b Signed/NR commits.
		return c
	}
	cp := cloneCommit(c)
	cp.LayerEntries = b.patchLayerEntries(op, cp.LayerEntries)
	return cp
}

// ---- byzSigmaRefusal ---------------------------------------------------

// All byz never broadcast their Phase-2a coordination message OR their
// commit (they contribute no σ partials at any layer).
type byzSigmaRefusal struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSigmaRefusal) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzSigmaRefusal) AllowPhase2aEmission(op twoab.OperatorID) bool {
	return !b.ByzSet.Contains(op)
}

// byzSigmaRefusal also suppresses cascade-emitted Phase-2b commits by
// dropping every outbound KindCommit at the AllowDelivery gate. This
// mirrors the OBFT pattern of suppressing the Commit broadcast entirely.
func (b byzSigmaRefusal) AllowDelivery(from, _ twoab.OperatorID, kind ct.MsgKind) bool {
	if b.ByzSet.Contains(from) && kind == ct.KindCommit {
		return false
	}
	return true
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

// Byz silences their own leader-layer (naturally NR-emits there via
// Phase-2a NRPlaintext entry), then injects a forged σ entry at the same
// layer inside their ValueMsg / NoValueMsg LayerEntries. Rule 1 must
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

func (b byzCrossSigning) injectForgedSigmaIntoEntries(s *sim, op twoab.OperatorID, entries []twoab.LayerEntry) []twoab.LayerEntry {
	leaderLayer := -1
	for k := 0; k < s.cfg.K; k++ {
		if s.operators[k%s.cfg.N] == op {
			leaderLayer = k
			break
		}
	}
	if leaderLayer < 0 || leaderLayer >= s.cfg.K-1 {
		return entries
	}
	out := make([]twoab.LayerEntry, len(entries))
	for i, e := range entries {
		out[i] = twoab.LayerEntry{
			Layer:   e.Layer,
			Kind:    e.Kind,
			V:       append(twoab.Value(nil), e.V...),
			Payload: append([]byte(nil), e.Payload...),
		}
	}
	for i, e := range out {
		if e.Layer != leaderLayer {
			continue
		}
		out[i] = twoab.LayerEntry{
			Layer:   leaderLayer,
			Kind:    twoab.LayerEntrySigmaChained,
			V:       append(twoab.Value{}, s.canonValues[leaderLayer]...),
			Payload: forgeSigmaPartialBytes(op, leaderLayer, s.canonValues[leaderLayer]),
		}
		break
	}
	return out
}

func (b byzCrossSigning) OverrideValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	if !b.ByzSet.Contains(op) {
		return v
	}
	cp := cloneValueMsg(v)
	cp.LayerEntries = b.injectForgedSigmaIntoEntries(s, op, cp.LayerEntries)
	return cp
}

func (b byzCrossSigning) OverrideUpgradeValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	return b.OverrideValueMsg(s, op, v)
}

func (b byzCrossSigning) OverrideNoValueMsg(s *sim, op twoab.OperatorID, nv *twoab.NoValueMsg) *twoab.NoValueMsg {
	if !b.ByzSet.Contains(op) {
		return nv
	}
	cp := cloneNoValueMsg(nv)
	cp.LayerEntries = b.injectForgedSigmaIntoEntries(s, op, cp.LayerEntries)
	return cp
}

func (b byzCrossSigning) OverrideCommit(s *sim, op twoab.OperatorID, c *twoab.Commit) *twoab.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	if c.Side != twoab.CommitSideNRDirect {
		return c
	}
	cp := cloneCommit(c)
	cp.LayerEntries = b.injectForgedSigmaIntoEntries(s, op, cp.LayerEntries)
	return cp
}

// ---- byzFakePlaintextSigma (Rule 5) -----------------------------------

// Byz emits a KindValue with a plaintext L0Partial (σ partial at L_0) on
// a V no leader broadcast. Rule 5 fires at receivers when the partial
// doesn't verify against the emitter's pubKeyShare on the claimed V
// (post Op5: Rule 5 attribution is to the EMITTER for bad L0Partial,
// distinct from the leader-attribution path for bad L0Witness in
// Phase-1 bundles). We patch the natural KindValue emission via
// OverrideValueMsg / OverrideUpgradeValueMsg.
type byzFakePlaintextSigma struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzFakePlaintextSigma) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakePlaintextSigma) OverrideValueMsg(_ *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	if !b.ByzSet.Contains(op) {
		return v
	}
	// Post Op5: σ partial at L_0 lives in ValueMsg.L0Partial. Inject a
	// forged partial on a fake V to trigger Rule 5 (fake plaintext σ at
	// L_0) at receivers. ObserveValueMsg's verifyAndPoolL0Partial fires
	// Rule 5 against the emitter when the partial fails verify against
	// the emitter's pubKeyShare on the claimed V.
	cp := cloneValueMsg(v)
	cp.V = append(twoab.Value{}, "byz-fake-V-at-L_0"...)
	cp.ValueRoot = twoab.ValueRoot(cp.V)
	cp.L0Partial = forgeSigmaPartialBytes(op, 0, []byte("byz-fake-V-at-L_0"))
	return cp
}

func (b byzFakePlaintextSigma) OverrideUpgradeValueMsg(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) *twoab.ValueMsg {
	return b.OverrideValueMsg(s, op, v)
}

// ---- byzCrossOnionEquivocation (Rule 3) -------------------------------

// Byz emits an additional Commit with a different L_0 σ-direction-on-V'
// (or a different SigmaChained entry at Layer). Each is delivered to the
// cluster; receivers fire Rule 3 (CrossCommitEquivocation) on the second
// observation.
type byzCrossOnionEquivocation struct {
	honestDefaults
	ByzSet byzSet
	Layer  int
}

func (b byzCrossOnionEquivocation) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// BuildExtraValueMsgs injects a distinct ValueMsg on V' at L_0 — directly
// detectable by ObserveValueMsg's cross-V check (Rule 3 / Rule 6a).
// Post Op5, σ partial at L_0 lives in ValueMsg.L0Partial, so the cross-σ-V
// equivocation primitive at L_0 lands here (it lived in BuildExtraCommits
// pre-Op5 when the σ partial was in KindCommit-Signed).
func (b byzCrossOnionEquivocation) BuildExtraValueMsgs(_ *sim, op twoab.OperatorID, v *twoab.ValueMsg) []*twoab.ValueMsg {
	if v == nil || !b.ByzSet.Contains(op) || b.Layer != 0 {
		return nil
	}
	primeV := []byte("byz-V-prime")
	cp := cloneValueMsg(v)
	cp.V = append(twoab.Value{}, primeV...)
	cp.ValueRoot = twoab.ValueRoot(cp.V)
	cp.L0Partial = forgeSigmaPartialBytes(op, 0, primeV)
	return []*twoab.ValueMsg{cp}
}

func (b byzCrossOnionEquivocation) BuildExtraCommits(_ *sim, op twoab.OperatorID, c *twoab.Commit) []*twoab.Commit {
	// L_k>0 cross-σ-V equivocation lands via NRDirect's LayerEntries —
	// post Op5, ValueMsg/NoValueMsg also carry LayerEntries at L_k>0, but
	// the catalog's L_k>0 byz scenario specifically uses Phase-2a NRDirect
	// (equivocation observed pre-emission) for consistency with v4.
	if c == nil || !b.ByzSet.Contains(op) || b.Layer <= 0 {
		return nil
	}
	if c.Side != twoab.CommitSideNRDirect {
		return nil
	}
	if b.Layer >= len(c.LayerEntries)+1 {
		return nil
	}
	primeV := []byte("byz-V-prime")
	cp := cloneCommit(c)
	for i := range cp.LayerEntries {
		if cp.LayerEntries[i].Layer != b.Layer {
			continue
		}
		cp.LayerEntries[i] = twoab.LayerEntry{
			Layer:   b.Layer,
			Kind:    twoab.LayerEntrySigmaChained,
			V:       append(twoab.Value{}, primeV...),
			Payload: forgeSigmaPartialBytes(op, b.Layer, primeV),
		}
		break
	}
	return []*twoab.Commit{cp}
}

// ---- byzPhase2EquivocateCrossV (Rule 6a, 2abOBFT-specific) -----------

// The byz operator emits its natural Phase-2a KindValue on V AND injects
// an EXTRA KindValue on a distinct V_b ≠ V. Each honest receiver
// observes TWO ValueMsgs from the same op with different V_0 fields →
// ObserveValueMsg's cross-V check fires Rule 6a (Phase2Equivocation) on
// the second observation. (Per spec §Authorized Phase-2 emission pairs,
// the same byz behavior would also be classified as cross-σ-V — but the
// receiver-side rule key in v4 is unified under Rule 6a.)
//
// LeaderBroadcastPlan unchanged (the byz, even if also the leader at
// some layer, broadcasts honestV in Phase 1). The deviation is purely
// at Phase 2a emission time, isolating the Phase-2-equivocation path
// from any L_0-leader equivocation. Catalog scenario uses op2 (a
// non-leader at L_0 by default rotation) to keep the test focused on
// the Phase-2 equivocation receiver-side detection.
//
// Cluster outcome: at n=4 f=1, the 3 honest ops (op1, op3, op4) all
// contribute to value_pool[V] → qV=3 reached trivially without needing
// the byz's contribution. σ-eligibility triggers → SuccessFastest at
// L_0. The cross-V extra ValueMsg is rejected at the cross-V check
// before the pool update; evidence is recorded but the cluster decides
// cleanly. The byz's natural KindValue on V also pools (it arrives
// either first or second relative to the cross-V extra; whichever wins
// the race is what pools).
//
// Assumption on byz natural-kind: BuildExtraValueMsgs returns nil
// unless natural Phase-2a was KindValue (v != nil). In CorrectnessProfile
// (deterministic delivery, honest leader, host valid), the byz always
// lands on KindValue at Phase 2a. Under stress sweeps with byz V-drop
// or host-flip, the byz could land on KindNoValue/NRDirect — in that
// case the cross-V extra is suppressed and Rule 6a doesn't fire that
// slot. The catalog Expect (SuccessFastest) still holds via the honest
// majority; the adapter tests (TestAdapter_Phase2EquivocateCrossV_*)
// pin CorrectnessProfile so KindValue path is guaranteed.
//
// Note on byz self-observation: the cross-V extra ValueMsg goes onto
// the wire alongside the natural one. The byz's own Instance has its
// pool already updated with its natural V from MaybeFirePhase2a; the
// extra is not self-observed (self-observation dedup short-circuits
// peer-arrival from own op). Rule 6a evidence is strictly receiver-
// side detection.
type byzPhase2EquivocateCrossV struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzPhase2EquivocateCrossV) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzPhase2EquivocateCrossV) BuildExtraValueMsgs(_ *sim, op twoab.OperatorID, v *twoab.ValueMsg) []*twoab.ValueMsg {
	// Nil-guard: events.go calls BuildExtraValueMsgs universally — v is
	// nil if the natural Phase-2a kind was NoValueMsg or Commit. This
	// pattern requires a natural ValueMsg to clone L_k entries from.
	if v == nil || !b.ByzSet.Contains(op) {
		return nil
	}
	// Inject an extra KindValue on a distinct V_b. The L_k>0 entries
	// are copied from the natural emission so the only divergence is
	// the L_0 V field — this isolates the cross-V Rule-6a detection
	// path (vs the L_k>0 mismatched-entries path).
	extra := cloneValueMsg(v)
	extra.V = append(twoab.Value{}, "byz-cross-V"...)
	extra.ValueRoot = twoab.ValueRoot(extra.V)
	return []*twoab.ValueMsg{extra}
}

// ---- byzPhase2DowngradeValueNoValue (Rule 6a, 2abOBFT-specific) ------

// The byz operator emits its natural Phase-2a KindValue on V AND ALSO
// injects an extra KindNoValue from the same op via the cross-kind
// BuildExtraNoValueMsgs hook. The sequence "KindValue → KindNoValue" is
// NOT in the authorized A1-A8 set per spec (only the reverse — A1's
// KindNoValue → KindValue upgrade — is allowed). Per spec §Receiver
// ordering tolerance, a KindValue+KindNoValue pair gets interpreted as
// an A1 upgrade reorder PROVIDED the L_k entries match between the two
// messages. We force a mismatch by injecting an all-Empty NoValueMsg
// alongside the natural ValueMsg, whose own L_k entries are SigmaChained
// at non-Empty layers (the natural emission's entries are derived from
// the op's actual retention state — at minimum, the L_{K-1} entry for
// any op that retains the K-1 leader's bundle is SigmaChained when
// host-valid). The L_k-mismatch trips ObserveNoValueMsg's check at the
// A1-reorder branch (or ObserveValueMsg's symmetric branch, depending
// on arrival order) and fires Rule 6a.
//
// Cluster outcome (n=4 f=1): leader broadcast is honest; 3 honest +
// 1 byz emit KindValue on V; byz also emits extra KindNoValue. The 3
// honest contributions to value_pool[V] reach qV=3 → σ-eligibility →
// SuccessFastest at L_0 regardless of byz's contribution. Rule 6a
// evidence accumulates at each honest receiver.
//
// LayerEntries shape: the injected KindNoValue carries all-Empty
// entries (K-1 of them). This is structurally valid per validation.go
// and doesn't accidentally trigger other rules (e.g., Rule 4 on garbage
// payloads); the violation is exactly the unauthorized cross-kind
// sequence, isolated for Rule 6a.
//
// Assumption: BuildExtraNoValueMsgs returns nil unless natural Phase-2a
// kind was KindValue (nv == nil AND vm exists). The current nil-guard
// is `nv != nil` only — which means an NRDirect natural emission
// (Commit; both vm and nv are nil) would ALSO trigger the extra
// injection. That's intentional: the resulting KindCommit-NRDirect +
// extra-KindNoValue sequence is still NOT in A1-A8 and still fires
// Rule 6a (per spec §Slashable Phase-2 equivocation: "KindCommit-NR-
// direct followed by any other emission"). Under CorrectnessProfile
// at n=4 healthy, the byz lands on KindValue path → cross-kind
// downgrade case is exercised; under stress sweeps with byz on
// NRDirect, the cross-kind post-NRDirect case fires instead. Both
// produce Rule 6a evidence.
type byzPhase2DowngradeValueNoValue struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzPhase2DowngradeValueNoValue) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzPhase2DowngradeValueNoValue) BuildExtraNoValueMsgs(s *sim, op twoab.OperatorID, nv *twoab.NoValueMsg) []*twoab.NoValueMsg {
	// Cross-kind injection: fires when natural Phase-2a kind was ValueMsg
	// (nv is nil — that's the trigger condition). When the natural kind
	// was already NoValueMsg, the byz is honest-on-this-emission and we
	// return nil — the byz can't "double up" by emitting two KindNoValues
	// since the natural one already covers that.
	if nv != nil || !b.ByzSet.Contains(op) {
		return nil
	}
	entries := make([]twoab.LayerEntry, 0, s.cfg.K-1)
	for k := 1; k < s.cfg.K; k++ {
		entries = append(entries, twoab.LayerEntry{Layer: k, Kind: twoab.LayerEntryEmpty})
	}
	return []*twoab.NoValueMsg{{
		ClusterID:    s.cfgTwoab.ClusterID,
		OperatorID:   op,
		Height:       s.cfgTwoab.Height,
		LayerEntries: entries,
	}}
}

// ---- byzLateLeaderBroadcast --------------------------------------------

// Byz leader broadcasts so late that first-observation at honest receivers
// lands well past the Phase-2a fire-instant. Per spec there is no T_commit
// hard wall in 2abOBFT — bundles are accepted at any in-slot offset — so
// honest receivers still retain the bundle. However, the bundle arrives
// AFTER each honest op's Phase-2a fire-time, so each op fires NoValue at
// Phase 2a. The A1 upgrade path (NoValue → Value) fires for any honest op
// that subsequently receives V_0 + host valid; cluster σ-eligibility may
// or may not reach depending on f-byz and timing.
type byzLateLeaderBroadcast struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzLateLeaderBroadcast) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// OverrideOwnPhase1Delay returns the byz leader's Phase-1 bundle dispatch
// delay relative to its scheduled fetch instant. The 6·BTT magnitude is
// inherited from the bare-OBFT adapter's analogous helper so cross-
// protocol comparison at the same byz parameter holds. In 2abOBFT
// terms:
//   - At BTT=200ms with the default RelayCutoff=4s, t0Broadcast is around
//     3.5s ≈ 17·BTT; Phase-2a fire is at ~3.6s ≈ 18·BTT. A 6·BTT (1.2s)
//     delay applied to the leader's scheduled broadcast pushes first
//     observation at every honest receiver well past 18·BTT, even after
//     subtracting per-hop network delay savings.
//   - Concretely: byz scheduled to broadcast at t0Broadcast → actual
//     broadcast at t0Broadcast + 6·BTT → first arrival at receivers ≈
//     t0Broadcast + 6·BTT + 1·BTT (network) = t0Broadcast + 7·BTT,
//     which lands past Phase 2a fire-time.
//   - Result: every honest op fires KindNoValue at Phase 2a (no V
//     retained at fire-time). The subsequent A1 upgrade path fires only
//     if the bundle arrives in time relative to the cluster's trigger
//     evaluation.
func (b byzLateLeaderBroadcast) OverrideOwnPhase1Delay(s *sim, leader twoab.OperatorID) time.Duration {
	if !b.ByzSet.Contains(leader) {
		return 0
	}
	return 6 * s.cfg.BTT
}

// ---- byzDelayedCommit --------------------------------------------------

type byzDelayedCommit struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzDelayedCommit) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzDelayedCommit) OverrideOwnCommitDispatchDelay(s *sim, op twoab.OperatorID) time.Duration {
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

// BuildExtraValueMsgs forges σ-direction KindValues claiming each peer's
// OperatorID. Post Op5, σ contributions live in KindValue.L0Partial, so
// the aggregator-bypass primitive lands here (it lived in
// BuildExtraCommits pre-Op5 when the σ partial was in KindCommit-Signed).
func (b byzAggregatorBypass) BuildExtraValueMsgs(s *sim, op twoab.OperatorID, v *twoab.ValueMsg) []*twoab.ValueMsg {
	if v == nil || !b.ByzSet.Contains(op) {
		return nil
	}
	primeV := []byte("byz-bypass-V-prime")
	primeRoot := twoab.ValueRoot(twoab.Value(primeV))
	forged := make([]*twoab.ValueMsg, 0, len(s.operators)-1)
	for _, other := range s.operators {
		if other == op {
			continue
		}
		cp := cloneValueMsg(v)
		cp.OperatorID = other
		cp.V = append(twoab.Value{}, primeV...)
		cp.ValueRoot = primeRoot
		cp.L0Partial = forgeSigmaPartialBytes(other, 0, primeV)
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

func cloneValueMsg(v *twoab.ValueMsg) *twoab.ValueMsg {
	cp := *v
	cp.V = append(twoab.Value(nil), v.V...)
	cp.L0Witness = append(twoab.Signature(nil), v.L0Witness...)
	cp.L0Partial = append(twoab.Signature(nil), v.L0Partial...)
	cp.LayerEntries = cloneLayerEntries(v.LayerEntries)
	return &cp
}

func cloneNoValueMsg(nv *twoab.NoValueMsg) *twoab.NoValueMsg {
	cp := *nv
	cp.LayerEntries = cloneLayerEntries(nv.LayerEntries)
	return &cp
}

func cloneCommit(c *twoab.Commit) *twoab.Commit {
	cp := *c
	cp.L0Value = append(twoab.Value(nil), c.L0Value...)
	cp.L0Partial = append(twoab.Signature(nil), c.L0Partial...)
	cp.LayerEntries = cloneLayerEntries(c.LayerEntries)
	return &cp
}

func cloneLayerEntries(entries []twoab.LayerEntry) []twoab.LayerEntry {
	if entries == nil {
		return nil
	}
	out := make([]twoab.LayerEntry, len(entries))
	for i, e := range entries {
		out[i] = twoab.LayerEntry{
			Layer:   e.Layer,
			Kind:    e.Kind,
			V:       append(twoab.Value(nil), e.V...),
			Payload: append([]byte(nil), e.Payload...),
		}
	}
	return out
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
