package obft

import (
	"crypto/sha256"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
)

// internalByz is the OBFT-specific byz interface. Each method has an honest
// default; concrete patterns selectively override.
//
// BuildExtraCommits returns ADDITIONAL commits to broadcast from `op` beyond
// the (OverrideCommit-modified) primary commit. Used for cross-onion
// equivocation (Rule 3) where byz emits two structurally-distinct Commits at
// the same slot.
//
// OverrideOwnPhase1Delay returns extra delay (added to the network model's
// delay) for the leader's own Phase-1 broadcast emission. Used by
// LateLeaderBroadcast to push the bundle past T_commit at all receivers.
type internalByz interface {
	LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan
	AllowCommitBroadcast(op obft.OperatorID) bool
	AllowCertificateBroadcast(op obft.OperatorID) bool
	AllowDelivery(from, to obft.OperatorID, kind ct.MsgKind) bool
	OverrideDelay(rng *mrand.Rand, from, to obft.OperatorID, kind ct.MsgKind) time.Duration
	OverrideCommit(s *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit
	BuildExtraCommits(s *sim, op obft.OperatorID, c *obft.Commit) []*obft.Commit
	OverrideOwnPhase1Delay(s *sim, leader obft.OperatorID) time.Duration
}

type broadcastPlan struct {
	V          obft.Value
	Recipients []obft.OperatorID // nil = all peers
}

// byzSet is the set of byzantine operator IDs for the current pattern. Per-
// kind patterns interpret the set per their semantics; iterating in
// ByzOperators order keeps determinism.
type byzSet struct {
	Members []obft.OperatorID
	Lookup  map[obft.OperatorID]bool
}

func newByzSet(ops []ct.OperatorID) byzSet {
	s := byzSet{
		Members: make([]obft.OperatorID, len(ops)),
		Lookup:  make(map[obft.OperatorID]bool, len(ops)),
	}
	for i, op := range ops {
		s.Members[i] = obft.OperatorID(op)
		s.Lookup[obft.OperatorID(op)] = true
	}
	return s
}

func (s byzSet) Contains(op obft.OperatorID) bool { return s.Lookup[op] }

// translateByz maps an abstract consensustest.ByzPattern to an OBFT-internal
// impl. All catalog kinds are OBFT-applicable; QBFT-only kinds would return
// ct.ErrNotApplicable.
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
			k = 3 // default: top 3 silent (covers BFT-comparison "multi-silent" row)
		}
		return byzMultiSilent{OnlyHonestLayer: k}, nil
	case ct.ByzEquivocate111:
		return byzEquivoc111{ByzSet: bs}, nil
	case ct.ByzEquivocateAllNR:
		return byzEquivocAllNR{ByzSet: bs}, nil
	case ct.ByzEquivocateSigmaLockedSplit:
		// Recipients positional convention: first half → V_a, second half →
		// V_b. Catalog scenario sets size 2f (f recipients per side) so the
		// σ-locked-split class holds at any cluster size. Default at f=1 / n=4:
		// {op2}→V_a, {op3}→V_b.
		var recipientsA, recipientsB []obft.OperatorID
		if len(p.Recipients) >= 2 {
			half := len(p.Recipients) / 2
			for _, r := range p.Recipients[:half] {
				recipientsA = append(recipientsA, obft.OperatorID(r))
			}
			for _, r := range p.Recipients[half:] {
				recipientsB = append(recipientsB, obft.OperatorID(r))
			}
		} else {
			recipientsA = []obft.OperatorID{2}
			recipientsB = []obft.OperatorID{3}
		}
		return byzEquivocSigmaLockedSplit{
			ByzSet:      bs,
			RecipientsA: recipientsA,
			RecipientsB: recipientsB,
		}, nil
	case ct.ByzHV1SelectiveDelivery:
		// Recipients carries the full list of honest ops that receive V.
		// Default (no Recipients set): single recipient op2 (f=1 / n=4).
		recipients := []obft.OperatorID{}
		if len(p.Recipients) > 0 {
			for _, r := range p.Recipients {
				recipients = append(recipients, obft.OperatorID(r))
			}
		} else {
			recipients = append(recipients, obft.OperatorID(2))
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
	case ct.ByzAggregatorBypass:
		return byzAggregatorBypass{ByzSet: bs}, nil
	case ct.ByzPartialEquivocation:
		// Recipients positional convention: all-but-last → V_a (size 2f),
		// last → V_b (size 1). Catalog scenario sets size 2f+1 so σ-pool on
		// V_a reaches qV at any cluster size.
		var recipientsA, recipientsB []obft.OperatorID
		if len(p.Recipients) >= 2 {
			for _, r := range p.Recipients[:len(p.Recipients)-1] {
				recipientsA = append(recipientsA, obft.OperatorID(r))
			}
			recipientsB = append(recipientsB, obft.OperatorID(p.Recipients[len(p.Recipients)-1]))
		} else {
			// Default at f=1 / n=4: {op2, op3} → V_a, {op4} → V_b.
			recipientsA = []obft.OperatorID{2, 3}
			recipientsB = []obft.OperatorID{4}
		}
		return byzPartialEquivocation{
			ByzSet:      bs,
			RecipientsA: recipientsA,
			RecipientsB: recipientsB,
		}, nil
	case ct.ByzGarbageMessages, ct.ByzExceedsRateLimit, ct.ByzOfflineDoubleVAttempt:
		// Reserved enum values — covered at other layers, not via the
		// scenario catalog. See ByzKind enum comments in
		// consensustest/byz.go for where each is actually exercised.
		return nil, ct.ErrNotApplicable
	default:
		return nil, fmt.Errorf("obft adapter: unknown ByzKind %v", p.Kind)
	}
}

// ---- honest defaults (mixin) -------------------------------------------

type honestDefaults struct{}

func (honestDefaults) AllowCommitBroadcast(obft.OperatorID) bool      { return true }
func (honestDefaults) AllowCertificateBroadcast(obft.OperatorID) bool { return true }
func (honestDefaults) AllowDelivery(_, _ obft.OperatorID, _ ct.MsgKind) bool {
	return true
}
func (honestDefaults) OverrideDelay(_ *mrand.Rand, _, _ obft.OperatorID, _ ct.MsgKind) time.Duration {
	return -1
}
func (honestDefaults) OverrideCommit(_ *sim, _ obft.OperatorID, c *obft.Commit) *obft.Commit {
	return c
}
func (honestDefaults) BuildExtraCommits(_ *sim, _ obft.OperatorID, _ *obft.Commit) []*obft.Commit {
	return nil
}
func (honestDefaults) OverrideOwnPhase1Delay(_ *sim, _ obft.OperatorID) time.Duration {
	return 0
}

// ---- byzNone -----------------------------------------------------------

type byzNone struct{ honestDefaults }

func (byzNone) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// ---- byzSilentLeader ---------------------------------------------------

// Any byz operator silences when they're a layer leader (regardless of
// which layer). Multi-byz: each byz silences their own layer-leader role.
type byzSilentLeader struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSilentLeader) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzMultiSilent ----------------------------------------------------

// Silences layers 0..OnlyHonestLayer-1 (topology-driven, not byz-driven).
// OnlyHonestLayer is the deepest silent layer + 1 — at that layer and
// deeper, the honest leader broadcasts.
type byzMultiSilent struct {
	honestDefaults
	OnlyHonestLayer int
}

func (b byzMultiSilent) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if layer < b.OnlyHonestLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzEquivoc111 -----------------------------------------------------

// Each byz that is a layer-0 leader emits a distinct V to each honest
// receiver. With multi-byz, both byz equivocate when they're L_0.
type byzEquivoc111 struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivoc111) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	others := make([]obft.OperatorID, 0, len(s.operators)-1)
	for _, op := range s.operators {
		if op != leader {
			others = append(others, op)
		}
	}
	plans := make([]broadcastPlan, 0, len(others))
	for i, recipient := range others {
		v := append(obft.Value{}, []byte{'b', 'y', 'z', '-', 'V', byte('1' + i)}...)
		plans = append(plans, broadcastPlan{V: v, Recipients: []obft.OperatorID{recipient}})
	}
	return plans
}

// ---- byzEquivocAllNR ---------------------------------------------------

// Each byz that is a layer-0 leader floods two distinct V's to all honest
// receivers (forces all to NR).
type byzEquivocAllNR struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzEquivocAllNR) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	all := s.operators
	return []broadcastPlan{
		{V: append(obft.Value{}, "byz-V-A"...), Recipients: all},
		{V: append(obft.Value{}, "byz-V-B"...), Recipients: all},
	}
}

// ---- byzEquivocSigmaLockedSplit ----------------------------------------

// σ-locked split equivocation: each byz that is a layer-0 leader emits V_a
// to RecipientsA, V_b to RecipientsB, ∅ to the rest. At f=1 / n=4 this is
// the canonical "1-1-NR" split (V_a → 1 op, V_b → 1 op, 1 op silent → NR).
// Generalized: |RecipientsA| = |RecipientsB| = f keeps the σ-locked-split
// slot-miss class at any cluster size — σ-pool on each V = f + leader's
// σ_L^V = f+1 < qV; NR-pool from the remaining N-1-2f silent honest = N-1-2f
// = 3f+1-1-2f = f < qEnc → MISS at L_0 with no fall-through.
type byzEquivocSigmaLockedSplit struct {
	honestDefaults
	ByzSet      byzSet
	RecipientsA []obft.OperatorID
	RecipientsB []obft.OperatorID
}

func (b byzEquivocSigmaLockedSplit) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	vA := append(obft.Value{}, "byz-V-A"...)
	vB := append(obft.Value{}, "byz-V-B"...)
	return []broadcastPlan{
		{V: vA, Recipients: append([]obft.OperatorID(nil), b.RecipientsA...)},
		{V: vB, Recipients: append([]obft.OperatorID(nil), b.RecipientsB...)},
	}
}

// ---- byzPartialEquivocation -------------------------------------------

// Natural-recovery equivocation: byz leader at L_0 emits V_a to RecipientsA
// (size 2f) and V_b to RecipientsB (size 1; one V_b recipient is sufficient
// to make this a real equivocation). The leader's instance σ-locks on V_a
// (first plan succeeds via BuildPhase1Bundle); σ_L^V(V_b) is forged by
// forgeByzBundle and emitted on V_b's bundle to its recipient. The leader's
// own Commit broadcast (Witnesses rehydration per spec §Phase 2) propagates
// σ_L^V(V_a) cluster-wide, including to V_b's recipient.
//
// Outcome at any cluster size: σ-pool on V_a = 2f recipients + leader's
// σ_L^V(V_a) = 2f+1 = qV → quorum reaches. σ-pool on V_b = 1 recipient +
// leader's σ_L^V(V_b) = 2 < qV (for f ≥ 1). Pigeonhole 2 holds: at most one
// V reaches qV cluster-wide. Slot SUCCEEDS at L_0 with V_a even though the
// leader equivocated (byz fumbled the timing — OBFT.md:443).
//
// Equivocation evidence (the V_a/V_b pair signed by the leader's key) is
// still gossipable as Rule 2 evidence — success at L_0 doesn't suppress
// slashing. This validates the "byz fumbles equivocation" recovery path
// distinct from the σ-locked split slot-miss covered by
// byzEquivocSigmaLockedSplit (OBFT.md:452).
//
// Silent no-op fallback: the pattern fires only when a byz operator is the
// L_0 layer leader. If the catalog scenario's byz placement is changed
// without updating the leader rotation (or sweep tests place byz outside
// the rotation), this pattern degrades to "healthy" — no equivocation is
// emitted. The catalog scenario assumes default rotation where op1 leads
// L_0; pair byz=op1 with default leader rotation to keep this pattern
// active.
type byzPartialEquivocation struct {
	honestDefaults
	ByzSet      byzSet
	RecipientsA []obft.OperatorID // get V_a (size 2f)
	RecipientsB []obft.OperatorID // get V_b (size 1)
}

func (b byzPartialEquivocation) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	vA := append(obft.Value{}, "byz-V-A"...)
	vB := append(obft.Value{}, "byz-V-B"...)
	return []broadcastPlan{
		{V: vA, Recipients: append([]obft.OperatorID(nil), b.RecipientsA...)},
		{V: vB, Recipients: append([]obft.OperatorID(nil), b.RecipientsB...)},
	}
}

// ---- byzHV1Selective ---------------------------------------------------

// h_V=f selective delivery: each byz that is a layer-0 leader sends V to
// exactly Recipients (sized to f at the catalog scenario's apply time so
// the pattern's "σ-pool < qV AND NR-pool < qEnc" outcome holds at any n).
// "HV1" reflects the f=1 / n=4 historical name; the generalized pattern
// delivers V to exactly f honest at any cluster size.
type byzHV1Selective struct {
	honestDefaults
	ByzSet     byzSet
	Recipients []obft.OperatorID
}

func (b byzHV1Selective) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if !b.ByzSet.Contains(leader) || layer != 0 {
		return []broadcastPlan{{V: honestV}}
	}
	return []broadcastPlan{
		{V: honestV, Recipients: append([]obft.OperatorID(nil), b.Recipients...)},
	}
}

// ---- byzFakeEncryptedPresence ------------------------------------------

// Each byz silences at SilentLayer (L_0 by default) and overrides their
// commit to inject a garbage encrypted entry at GarbageLayer.
type byzFakeEncryptedPresence struct {
	honestDefaults
	ByzSet       byzSet
	SilentLayer  int
	GarbageLayer int
}

func (b byzFakeEncryptedPresence) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) && layer == b.SilentLayer {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakeEncryptedPresence) OverrideCommit(_ *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	if b.GarbageLayer < 0 || b.GarbageLayer >= len(c.Layers) {
		return c
	}
	cp := cloneCommit(c)
	cp.Layers[b.GarbageLayer] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, "byz-fake-V-at-deeper-layer"...),
		Ciphertext: []byte("garbage-bytes-not-a-valid-ibe-ciphertext"),
	}
	return cp
}

// ---- byzSigmaRefusal ---------------------------------------------------

// All byz never broadcast Commits (and thus contribute no σ partials).
type byzSigmaRefusal struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzSigmaRefusal) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzSigmaRefusal) AllowCommitBroadcast(op obft.OperatorID) bool {
	return !b.ByzSet.Contains(op)
}

// ---- byzWithholdLeader (deepest-layer leader silent) -------------------

// Each byz silences ONLY at the deepest layer they lead. Tests the Class A
// late-deepest-layer pathology — at K = f+2 the cluster has exactly 2
// honest leaders so silencing the deepest one means the honest one before
// must broadcast in time. At K = f+1 (< MinK) the silenced byz at L_{K-1}
// has no honest fall-through.
//
// Silent no-op fallback: at K < N (or with a byz operator that doesn't lead
// any layer at all, e.g. byz=op N at K=2 where rotation only covers ops 1, 2),
// no byz is the deepest-layer leader and the pattern degrades to "healthy"
// for all leaders. The catalog scenario assumes the production K=N convention
// where every operator leads exactly one layer; sweep tests using K < N
// should not pair this pattern with byz IDs outside the leader rotation
// (translateByz emits a runtime warning via the assertion below).
type byzWithholdLeader struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzWithholdLeader) LeaderBroadcastPlan(s *sim, leader obft.OperatorID, layer int, honestV obft.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) && layer == s.cfg.K-1 {
		return nil
	}
	return []broadcastPlan{{V: honestV}}
}

// ---- byzCertWithholding (byz refuses cert broadcast) -------------------

// Byz reconstructs the certificate locally but doesn't gossip it. Tests the
// cert-gossip fallback path: an honest receiver who didn't reconstruct
// locally relies on cert gossip from peers; if the byz is the only
// reconstructor and refuses to gossip, that op should fall through to
// some other path (or miss).
//
// At any n the f-1 other honest operators still gossip their certs, so a
// single cert-withholding byz never breaks healthy-path liveness — the
// scenario passes regardless of n / cluster size.
type byzCertWithholding struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzCertWithholding) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}
func (b byzCertWithholding) AllowCertificateBroadcast(op obft.OperatorID) bool {
	return !b.ByzSet.Contains(op)
}

// ---- byzCrossSigning (Rule 1 evidence) ---------------------------------

// Each byz silences their own leader-layer (so they emit a REAL NR partial
// at that layer per silent-leader semantics), then OverrideCommit injects a
// forged σ entry at the same layer. The σ at L_k>0 is IBE-wrapped — its
// "ciphertext" bytes don't need to verify until Phase 3 chain-decrypt — so
// the cross-signing detection fires on the σ + NR collision before any
// per-σ verification gate. Rule 1 must target a layer < K-1 (no NR-tag at
// deepest layer); the adapter's leader rotation is `operators[k % N]` (slot
// is constant in the sim), so byz=op[i] leads layer k where k % N == i-1.
type byzCrossSigning struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzCrossSigning) LeaderBroadcastPlan(_ *sim, leader obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	if b.ByzSet.Contains(leader) {
		return nil // byz silent at own leader-layer → naturally NR-emits there
	}
	return []broadcastPlan{{V: honestV}}
}

func (b byzCrossSigning) OverrideCommit(s *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	leaderLayer := -1
	for k := 0; k < s.cfg.K; k++ {
		if s.operators[k%s.cfg.N] == op {
			leaderLayer = k
			break
		}
	}
	if leaderLayer < 0 || leaderLayer >= s.cfg.K-1 {
		// No NR tag at deepest layer; Rule 1 unrepresentable here.
		return c
	}
	cp := cloneCommit(c)
	cp.Layers[leaderLayer] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, s.canonValues[leaderLayer]...),
		Ciphertext: forgeSigmaPartialBytes(op, leaderLayer, s.canonValues[leaderLayer]),
	}
	return cp
}

// ---- byzFakePlaintextSigma (Rule 5 evidence) ---------------------------

// Each byz emits a plaintext σ partial at L_0 on a V no Phase-1 leader
// produced. Honest receivers at retained-V detect this immediately as
// cryptoFake and fire Rule 5 evidence.
type byzFakePlaintextSigma struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzFakePlaintextSigma) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzFakePlaintextSigma) OverrideCommit(_ *sim, op obft.OperatorID, c *obft.Commit) *obft.Commit {
	if !b.ByzSet.Contains(op) {
		return c
	}
	if len(c.Layers) == 0 {
		return c
	}
	cp := cloneCommit(c)
	// Fake V at L_0 — distinct from any leader-broadcast canon V.
	cp.Layers[0] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, "byz-fake-V-at-L_0"...),
		Ciphertext: forgeSigmaPartialBytes(op, 0, []byte("byz-fake-V-at-L_0")),
	}
	return cp
}

// ---- byzCrossOnionEquivocation (Rule 3 per-layer evidence) -------------

// Each byz emits TWO Commits at the same slot, with σ partials on
// different V's at Layer. Detected at honest receivers (the second
// distinct emission triggers Rule 3 per-layer evidence).
type byzCrossOnionEquivocation struct {
	honestDefaults
	ByzSet byzSet
	Layer  int
}

func (b byzCrossOnionEquivocation) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzCrossOnionEquivocation) BuildExtraCommits(s *sim, op obft.OperatorID, c *obft.Commit) []*obft.Commit {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	if b.Layer < 0 || b.Layer >= len(c.Layers) {
		return nil
	}
	cp := cloneCommit(c)
	primeV := []byte("byz-V-prime")
	cp.Layers[b.Layer] = obft.EncryptedLayer{
		Value:      append(obft.Value{}, primeV...),
		Ciphertext: forgeSigmaPartialBytes(op, b.Layer, primeV),
	}
	return []*obft.Commit{cp}
}

// ---- byzLateLeaderBroadcast (spec Class A: asymmetric propagation) ------

// Each byz that is a layer leader broadcasts their Phase-1 bundle so late
// that first-observation at any honest receiver lands past T_commit. Per
// spec §Failure modes / Class A: honest treats as silent-leader, NR-emits;
// if cluster reaches NR-quorum at this layer, fall-through to next layer.
// At byz=op1 (L_0 leader) under default rotation: L_0 σ-pool insufficient
// (only byz's own Phase-1 σ_V counts; honest reject the bundle as past-
// T_commit), NR-quorum at L_0 unlocks L_1 → L_1 honest leader's bundle
// propagates on time → decide at L_1. Tests the spec's headline
// "fall-through to deeper backup whose `B_{k+1} + slack` is wider" claim.
type byzLateLeaderBroadcast struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzLateLeaderBroadcast) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

// OverrideOwnPhase1Delay returns a BTT-scaled delay (6×BTT) so the byz
// leader's bundle arrives past T_commit at every honest receiver regardless
// of operating point. The factor 6 covers the deepest K=4 absorption window
// (`B_3 + slack = 5.5 BTT`); for L_0 (`B_0 + slack = 1 BTT`) this is 6×
// over. Hardcoded delay would silently fail at BTT > 200ms.
func (b byzLateLeaderBroadcast) OverrideOwnPhase1Delay(s *sim, leader obft.OperatorID) time.Duration {
	if !b.ByzSet.Contains(leader) {
		return 0
	}
	return 6 * s.cfg.BTT
}

// ---- byzAggregatorBypass (negative test for safety machinery) ----------

// Byz emits forged "extra" commits claiming to be from each non-leader
// honest operator, with a σ entry on a distinct V_prime at L_0. Combined
// with honest σ-quorum on the canonical V at L_0, the OfflineAggregator
// observes ≥ qV plaintext σ partials on V_prime (forged identities) AND
// ≥ qV plaintext σ partials on the canonical V → reconstructs both →
// NoOfflineDoubleV violation → SafetyPanic.
//
// This is a NEGATIVE test pattern: it deliberately produces a safety
// violation to verify the framework's panic-on-violation enforcement.
// Should NOT be added to the catalog (matrix tests would crash); use only
// in standalone tests with the expected panic behavior asserted.
type byzAggregatorBypass struct {
	honestDefaults
	ByzSet byzSet
}

func (b byzAggregatorBypass) LeaderBroadcastPlan(_ *sim, _ obft.OperatorID, _ int, honestV obft.Value) []broadcastPlan {
	return []broadcastPlan{{V: honestV}}
}

func (b byzAggregatorBypass) BuildExtraCommits(s *sim, op obft.OperatorID, c *obft.Commit) []*obft.Commit {
	if !b.ByzSet.Contains(op) {
		return nil
	}
	primeV := []byte("byz-bypass-V-prime")
	forged := make([]*obft.Commit, 0, len(s.operators)-1)
	// Forge from every other operator (including the L_0 leader). The
	// aggregator credits forged identities by c.OperatorID, so we need at
	// least qV = 2f+1 distinct claimed-sender IDs to reach the V_prime
	// reconstruction quorum. Forging from leader doesn't conflict with
	// honest L_0 σ-quorum: the aggregator's per-bucket-per-op dedup would
	// still let leader appear in BOTH V_canonical (via Phase-1 σ_V) and
	// V_prime (via this forge), producing two reconstructable buckets —
	// exactly what NoOfflineDoubleV detects.
	for _, other := range s.operators {
		if other == op {
			continue // self already emits the canonical commit
		}
		cp := cloneCommit(c)
		cp.OperatorID = other // forged claimed sender
		cp.Layers[0] = obft.EncryptedLayer{
			Value:      append(obft.Value{}, primeV...),
			Ciphertext: forgeSigmaPartialBytes(other, 0, primeV),
		}
		forged = append(forged, cp)
	}
	return forged
}

// ---- helpers -----------------------------------------------------------

// clonePhase1Bundle deep-copies a Phase1Bundle so each per-recipient arrival
// event holds an independent copy. Mirrors the QBFT adapter's per-recipient
// DeepCopy pattern; protects against future receiver-side mutations
// corrupting other receivers' inbound data.
func clonePhase1Bundle(b *obft.Phase1Bundle) *obft.Phase1Bundle {
	cp := *b
	cp.Value = append(obft.Value(nil), b.Value...)
	cp.SigmaV = append(obft.Signature(nil), b.SigmaV...)
	return &cp
}

// cloneCertificate deep-copies a Certificate so each per-recipient arrival
// event holds an independent copy. See clonePhase1Bundle.
func cloneCertificate(c *obft.Certificate) *obft.Certificate {
	cp := *c
	cp.Value = append(obft.Value(nil), c.Value...)
	cp.Signature = append(obft.Signature(nil), c.Signature...)
	return &cp
}

// cloneCommit deep-copies a Commit so OverrideCommit / BuildExtraCommits
// don't mutate the honest-built original.
func cloneCommit(c *obft.Commit) *obft.Commit {
	cp := &obft.Commit{
		ClusterID:  c.ClusterID,
		OperatorID: c.OperatorID,
		Height:     c.Height,
		Layers:     make([]obft.EncryptedLayer, len(c.Layers)),
		NRPartials: make([]obft.NRPartial, len(c.NRPartials)),
		Witnesses:  make([]obft.LeaderSigmaWitness, len(c.Witnesses)),
	}
	for i, el := range c.Layers {
		cp.Layers[i] = obft.EncryptedLayer{
			Value:      append(obft.Value(nil), el.Value...),
			Ciphertext: append([]byte(nil), el.Ciphertext...),
		}
	}
	for i, nr := range c.NRPartials {
		cp.NRPartials[i] = obft.NRPartial{
			Layer:      nr.Layer,
			PartialSig: append(obft.Signature(nil), nr.PartialSig...),
		}
	}
	for i, w := range c.Witnesses {
		cp.Witnesses[i] = obft.LeaderSigmaWitness{
			Layer:  w.Layer,
			Leader: w.Leader,
			Value:  append(obft.Value(nil), w.Value...),
			SigmaV: append(obft.Signature(nil), w.SigmaV...),
		}
	}
	return cp
}

// forgeSigmaPartialBytes returns deterministic bytes shaped like a real BLS
// partial signature, but not cryptographically valid. Honest receivers
// in stub-BLS mode treat structure-passing bytes as a "claim" and run
// per-rule detection on the (op, layer, value) tuple — exactly what the
// per-rule evidence machinery needs to fire. Byte content depends on (op,
// layer, value) so two distinct V's at the same (op, layer) produce
// distinct bytes (mirroring real BLS's V-dependent signatures).
func forgeSigmaPartialBytes(op obft.OperatorID, layer int, value []byte) []byte {
	h := sha256.Sum256(append(append([]byte{byte(op), byte(op >> 8), byte(layer), byte(layer >> 8)}, value...), 0xff))
	out := make([]byte, ct.StubSignatureSize)
	out[0] = 0xff // distinguishes byz-forged from honest stubs
	for i := 1; i < len(out); i++ {
		out[i] = h[i%len(h)] ^ byte(i)
	}
	return out
}
