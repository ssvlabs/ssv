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

// Byz emits a Commit-Signed with a plaintext σ partial at L_0 on a V no
// leader broadcast. Rule 5 fires at receivers when the partial doesn't
// verify against any retained V at L_0. The dynamic Commit emission
// fires through the afterStateDelta cascade; we patch it via
// OverrideCommit.
type byzFakePlaintextSigma struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzFakePlaintextSigma) LeaderBroadcastPlan(_ *sim, _ twoab.OperatorID, _ int, honestV twoab.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakePlaintextSigma) OverrideCommit(_ *sim, op twoab.OperatorID, c *twoab.Commit) *twoab.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	// Inject a fake plaintext σ at L_0: only meaningful for Side=Signed
	// (where L0Value + L0Partial carry σ-direction-on-V_0).
	if c.Side != twoab.CommitSideSigned {
		return c
	}
	cp := cloneCommit(c)
	cp.L0Value = append(twoab.Value{}, "byz-fake-V-at-L_0"...)
	cp.L0Partial = forgeSigmaPartialBytes(op, 0, []byte("byz-fake-V-at-L_0"))
	return cp
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

func (b byzCrossOnionEquivocation) BuildExtraCommits(s *sim, op twoab.OperatorID, c *twoab.Commit) []*twoab.Commit {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	primeV := []byte("byz-V-prime")
	switch {
	case b.Layer == 0 && c.Side == twoab.CommitSideSigned:
		// Inject a distinct Signed commit on V' at L_0 — directly
		// detectable by ObserveCommit's cross-V check.
		cp := cloneCommit(c)
		cp.L0Value = append(twoab.Value{}, primeV...)
		cp.L0Partial = forgeSigmaPartialBytes(op, 0, primeV)
		return []*twoab.Commit{cp}
	case b.Layer > 0 && c.Side == twoab.CommitSideNRDirect:
		// L_k>0 cross-σ-V equivocation can only land via NRDirect's
		// LayerEntries — Signed and NR commits carry no L_k>0 entries.
		if b.Layer < 0 || b.Layer >= len(c.LayerEntries)+1 {
			return nil
		}
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
	default:
		return nil
	}
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

func (b byzAggregatorBypass) BuildExtraCommits(s *sim, op twoab.OperatorID, c *twoab.Commit) []*twoab.Commit {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	if c.Side != twoab.CommitSideSigned {
		// Forged-identity bypass only meaningful when we can pose as a
		// σ-signing peer on V_prime. NR / NRDirect commits don't
		// contribute to the aggregator's σ-pool.
		return nil
	}
	primeV := []byte("byz-bypass-V-prime")
	forged := make([]*twoab.Commit, 0, len(s.operators)-1)
	for _, other := range s.operators {
		if other == op {
			continue
		}
		cp := cloneCommit(c)
		cp.OperatorID = other
		cp.Side = twoab.CommitSideSigned
		cp.L0Value = append(twoab.Value{}, primeV...)
		cp.L0Partial = forgeSigmaPartialBytes(other, 0, primeV)
		cp.LayerEntries = nil
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
