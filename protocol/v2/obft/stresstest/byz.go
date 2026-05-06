package stresstest

import (
	mrand "math/rand"
	"time"

	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// ByzPattern parameterizes a byzantine operator's behavior. Each method
// returns a "no override / honest" answer by default (-1 delay, true to
// allow, identity transform, plain leader broadcast plan); patterns that
// model byz behavior selectively override.
//
// Conventions:
//   - The byzantine operator (if any) is fixed at OperatorID(1) by default
//     so leader-rotation puts them at L_0 for slot 1 (which is what the
//     OBFT spec analyses target). Specific patterns may override Byz()
//     to point elsewhere when modeling a non-L_0 byz.
//   - Network-failure patterns (silent, multi-silent) suppress the leader
//     broadcast itself, modeled through LeaderBroadcastPlan returning empty.
//   - Selective-delivery / equivocation patterns return one or two plans
//     with explicit Recipients lists.
//   - σ-side wire forgery (h_V=1's fake plaintext σ at L_0) goes through
//     OverrideCommit at Phase-2 emission time.
type ByzPattern interface {
	// LeaderBroadcastPlan decides what bundles `leader` will broadcast at
	// the Phase-1 fetch event. Returns empty for silent leaders. Returns
	// multiple plans for equivocation. `honestV` is the canonical V the
	// honest leader would have fetched; byz patterns override with V'/V''.
	LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan

	// AllowCommitBroadcast is false for operators that suppress their
	// Phase-2 Onion (e.g., σ-refusal byz).
	AllowCommitBroadcast(op obft.OperatorID) bool


	// AllowCertificateBroadcast is false for operators that suppress
	// their final-certificate gossip.
	AllowCertificateBroadcast(op obft.OperatorID) bool

	// AllowDelivery is false to selectively drop (sender → receiver) for
	// the given message kind. Used by selective-delivery patterns.
	AllowDelivery(from, to obft.OperatorID, kind MsgKind) bool

	// OverrideDelay returns a non-negative delay to use instead of the
	// network model's; -1 means "no override; use the network model".
	OverrideDelay(rng *mrand.Rand, from, to obft.OperatorID, kind MsgKind) time.Duration

	// OverrideCommit lets a byz-emitter swap their Phase-2 Onion contents
	// (e.g., insert a fake plaintext σ at L_0). Returns the Onion to
	// actually broadcast.
	OverrideCommit(s *sim, op obft.OperatorID, o *obft.Commit) *obft.Commit

}

// BroadcastPlan is a leader's per-broadcast decision: which V to send,
// and to whom. Recipients == nil means "all peers (per network model)".
type BroadcastPlan struct {
	V          obft.Value
	Recipients []obft.OperatorID
}

// ---- ByzNone (no byz; every leader is honest) ----------------------------

type ByzNone struct{}

func (ByzNone) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []BroadcastPlan {
	return []BroadcastPlan{{V: honestV}}
}
func (ByzNone) AllowCommitBroadcast(obft.OperatorID) bool                                      { return true }
func (ByzNone) AllowCertificateBroadcast(obft.OperatorID) bool                                { return true }
func (ByzNone) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                            { return true }
func (ByzNone) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration    { return -1 }
func (ByzNone) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit            { return o }

// ---- ByzSilentLeader (byz is leader at one layer, broadcasts nothing) ---

type ByzSilentLeader struct {
	Byz   obft.OperatorID
	Layer int
}

func (b ByzSilentLeader) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader == b.Byz && layer == b.Layer {
		return nil // silent
	}
	return []BroadcastPlan{{V: honestV}}
}
func (ByzSilentLeader) AllowCommitBroadcast(obft.OperatorID) bool                                   { return true }
func (ByzSilentLeader) AllowCertificateBroadcast(obft.OperatorID) bool                             { return true }
func (ByzSilentLeader) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                         { return true }
func (ByzSilentLeader) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration { return -1 }
func (ByzSilentLeader) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit         { return o }

// ---- ByzMultiSilent (every leader except the deepest layer is silent) ---

// ByzMultiSilent makes layers [0, OnlyHonestLayer) silent; the leader at
// OnlyHonestLayer broadcasts honestly.
type ByzMultiSilent struct{ OnlyHonestLayer int }

func (b ByzMultiSilent) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if layer < b.OnlyHonestLayer {
		return nil
	}
	return []BroadcastPlan{{V: honestV}}
}
func (ByzMultiSilent) AllowCommitBroadcast(obft.OperatorID) bool                                   { return true }
func (ByzMultiSilent) AllowCertificateBroadcast(obft.OperatorID) bool                             { return true }
func (ByzMultiSilent) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                         { return true }
func (ByzMultiSilent) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration { return -1 }
func (ByzMultiSilent) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit         { return o }

// ---- ByzEquivocSigmaLockedSplit (1-1-Defer at L_0) ---------------------

// ByzEquivocSigmaLockedSplit: byz is leader at layer 0. They deliver V_a
// to RecipientA only, V_b to RecipientB only, nothing to the rest.
// Models the "σ-locked split" pattern from BFT-comparison Table 3.
type ByzEquivocSigmaLockedSplit struct {
	Byz        obft.OperatorID
	RecipientA obft.OperatorID
	RecipientB obft.OperatorID
}

func (b ByzEquivocSigmaLockedSplit) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []BroadcastPlan{{V: honestV}}
	}
	vA := append(obft.Value{}, "byz-V-A"...)
	vB := append(obft.Value{}, "byz-V-B"...)
	return []BroadcastPlan{
		{V: vA, Recipients: []obft.OperatorID{b.Byz, b.RecipientA}},
		{V: vB, Recipients: []obft.OperatorID{b.Byz, b.RecipientB}},
	}
}
func (ByzEquivocSigmaLockedSplit) AllowCommitBroadcast(obft.OperatorID) bool                                   { return true }
func (ByzEquivocSigmaLockedSplit) AllowCertificateBroadcast(obft.OperatorID) bool                             { return true }
func (ByzEquivocSigmaLockedSplit) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                         { return true }
func (ByzEquivocSigmaLockedSplit) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration { return -1 }
func (ByzEquivocSigmaLockedSplit) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit         { return o }

// ---- ByzEquivocAllDefer (delivers both V's to all 3 honest) -------------

// ByzEquivocAllDefer floods both V_a and V_b to every honest peer. Each
// honest retains both → Defer-due-to-equivocation → force-NR at end of
// Phase 2 → NR-quorum at L_0 → fall-through to L_1.
type ByzEquivocAllDefer struct{ Byz obft.OperatorID }

func (b ByzEquivocAllDefer) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []BroadcastPlan{{V: honestV}}
	}
	all := s.operators
	return []BroadcastPlan{
		{V: append(obft.Value{}, "byz-V-A"...), Recipients: all},
		{V: append(obft.Value{}, "byz-V-B"...), Recipients: all},
	}
}
func (ByzEquivocAllDefer) AllowCommitBroadcast(obft.OperatorID) bool                                   { return true }
func (ByzEquivocAllDefer) AllowCertificateBroadcast(obft.OperatorID) bool                             { return true }
func (ByzEquivocAllDefer) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                         { return true }
func (ByzEquivocAllDefer) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration { return -1 }
func (ByzEquivocAllDefer) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit         { return o }

// ---- ByzEquivoc111 (1-1-1 split: each honest gets a unique V) -----------

// ByzEquivoc111: byz delivers V_1 to op2 only, V_2 to op3 only, V_3 to op4
// only. Each non-leader honest σ-locks on a distinct V before they can
// observe equivocation. σ-pools split below qV; byz also refuses to NR
// (no honest NR can join either).
type ByzEquivoc111 struct{ Byz obft.OperatorID }

func (b ByzEquivoc111) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []BroadcastPlan{{V: honestV}}
	}
	others := make([]obft.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != b.Byz {
			others = append(others, op)
		}
	}
	plans := make([]BroadcastPlan, 0, len(others))
	for i, recipient := range others {
		v := append(obft.Value{}, []byte{'b', 'y', 'z', '-', 'V', byte('1' + i)}...)
		plans = append(plans, BroadcastPlan{V: v, Recipients: []obft.OperatorID{b.Byz, recipient}})
	}
	return plans
}
func (ByzEquivoc111) AllowCommitBroadcast(obft.OperatorID) bool                                   { return true }
func (ByzEquivoc111) AllowCertificateBroadcast(obft.OperatorID) bool                             { return true }
func (ByzEquivoc111) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                         { return true }
func (ByzEquivoc111) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration { return -1 }
func (ByzEquivoc111) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit         { return o }

// ---- ByzFakeEncryptedPresence (Rule 4 at k > 0) -------------------------

// ByzFakeEncryptedPresence models the spec §Phase 2 / Garbage-encryption
// deterrence scenario. The byz operator is the L_0 leader; they suppress
// their Phase-1 broadcast (so NR-quorum at L_0 reaches and decryption
// unlocks the next layer), and they substitute garbage bytes for their
// own Onion entry at GarbageLayer (a layer > 0 reached by NR-quorum
// fall-through).
//
// Expected outcome: the protocol falls through to the layer with an
// honest leader; receivers attempt to decrypt byz's Onion entry at
// GarbageLayer once chain decryption unlocks it; decryption fails (or
// decrypts to bytes that don't verify as a σ partial); Rule 4 evidence
// (EvidenceFakeEncryptedPresence) is recorded against the byz operator.
type ByzFakeEncryptedPresence struct {
	Byz          obft.OperatorID
	SilentLayer  int // byz suppresses Phase-1 here (typically 0)
	GarbageLayer int // byz substitutes garbage at this layer (must be > SilentLayer)
}

func (b ByzFakeEncryptedPresence) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader == b.Byz && layer == b.SilentLayer {
		return nil // suppress
	}
	return []BroadcastPlan{{V: honestV}}
}
func (b ByzFakeEncryptedPresence) AllowCommitBroadcast(obft.OperatorID) bool                                { return true }
func (b ByzFakeEncryptedPresence) AllowCertificateBroadcast(obft.OperatorID) bool                          { return true }
func (b ByzFakeEncryptedPresence) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                      { return true }
func (b ByzFakeEncryptedPresence) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration {
	return -1
}
func (b ByzFakeEncryptedPresence) OverrideCommit(_ *sim, op obft.OperatorID, o *obft.Commit) *obft.Commit {
	if op != b.Byz {
		return o
	}
	if b.GarbageLayer < 0 || b.GarbageLayer >= len(o.Layers) {
		return o
	}
	// Deep-copy the Commit so we don't mutate state retained elsewhere.
	cp := &obft.Commit{
		OperatorID: o.OperatorID,
		Height:     o.Height,
		Layers:     make([]obft.EncryptedLayer, len(o.Layers)),
		NRPartials: append([]obft.NRPartial{}, o.NRPartials...),
	}
	copy(cp.Layers, o.Layers)
	// Substitute the GarbageLayer entry with bytes that won't match the
	// stub IBE format, forcing chainDecryptForLayer to error → Rule 4.
	cp.Layers[b.GarbageLayer] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, "byz-fake-V-at-deeper-layer"...),
		Ciphertext: []byte("garbage-bytes-not-a-valid-stub-ibe-ciphertext"),
	}
	return cp
}

// ---- ByzHV1SelectiveDelivery (h_V=1 deadlock) ----------------------------

// ByzHV1SelectiveDelivery models the spec §Failure modes / "Byzantine
// selective-delivery grief at end of Phase 2 (h_V = 1 deadlock)". The
// byz operator is the L_0 leader. They:
//
//  1. Selectively deliver the Phase-1 bundle to exactly one honest
//     operator (Recipient); other honest never see the bundle.
//  2. Emit a normal Phase-2 Onion with their σ partial at L_0 (auth-signed
//     via the outer SSVMessage layer in production; in this harness the
//     OperatorID claim suffices).
//  3. Refuse to NR-emit at L_0 (cross-phase exclusivity since they're
//     σ-emitted at L_0).
//
// Expected outcome (per BFT-comparison.md Table 3 row "h_V=1 selective-
// delivery deadlock: ✗ slot miss"):
//
//   - σ-pool at L_0 = 1 (Recipient's σ) + byz's σ_L^V (visible only to
//     Recipient, who has the bundle retained) = 2 < qV=3.
//   - NR-pool at L_0 = (n - 2) honest force-NR + 0 byz = 2 < qEnc=3.
//   - Neither quorum reaches; fall-through to L_1 blocked.
//   - Slot misses cleanly.
type ByzHV1SelectiveDelivery struct {
	Byz       obft.OperatorID
	Recipient obft.OperatorID
}

func (b ByzHV1SelectiveDelivery) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []BroadcastPlan {
	if leader != b.Byz || layer != 0 {
		return []BroadcastPlan{{V: honestV}}
	}
	// Selective delivery: bundle reaches only the Recipient (and self,
	// for self-observation in BuildPhase1Bundle).
	return []BroadcastPlan{
		{V: honestV, Recipients: []obft.OperatorID{b.Byz, b.Recipient}},
	}
}
func (ByzHV1SelectiveDelivery) AllowCommitBroadcast(obft.OperatorID) bool                                { return true }
func (ByzHV1SelectiveDelivery) AllowCertificateBroadcast(obft.OperatorID) bool                          { return true }
func (ByzHV1SelectiveDelivery) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                      { return true }
func (ByzHV1SelectiveDelivery) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration {
	return -1
}
func (ByzHV1SelectiveDelivery) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit { return o }

// ---- ByzSigmaRefusal (byz never σ-emits; never NRs) --------------------

// ByzSigmaRefusal models the "σ-refusal coordinated with mesh-flake" byz
// from BFT-comparison Table 3. The byz operator is silent at the wire
// level on σ side AND on NR side (refuses to advance fall-through).
type ByzSigmaRefusal struct{ Byz obft.OperatorID }

func (b ByzSigmaRefusal) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, _ int, honestV obft.Value) []BroadcastPlan {
	return []BroadcastPlan{{V: honestV}} // byz happens not to be a leader; if they are, default-honest fetch
}
func (b ByzSigmaRefusal) AllowCommitBroadcast(op obft.OperatorID) bool                                   { return op != b.Byz }
func (ByzSigmaRefusal) AllowCertificateBroadcast(obft.OperatorID) bool                                  { return true }
func (ByzSigmaRefusal) AllowDelivery(_, _ obft.OperatorID, _ MsgKind) bool                              { return true }
func (ByzSigmaRefusal) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration      { return -1 }
func (ByzSigmaRefusal) OverrideCommit(_ *sim, _ obft.OperatorID, o *obft.Commit) *obft.Commit              { return o }
