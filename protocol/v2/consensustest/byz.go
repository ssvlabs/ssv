package consensustest

// ByzPattern is the framework-level abstract byzantine pattern. Per-protocol
// adapters translate Kind + params to their internal byz model; kinds an
// adapter can't faithfully translate cause Run to return ErrNotApplicable.
//
// ByzOperators carries the f-bound list of byzantine operators. Patterns
// document how multiple byz interact in their adapter translation:
//   - SilentLeader / SigmaRefusal: each byz is independently silent / refuses.
//   - MultiSilent: TOPOLOGY-driven, NOT operator-driven — silences whichever
//     operators happen to lead the first K layers (typically all honest at
//     the production K=N convention). ByzOperators is unused; K field selects
//     how many top layers stay silent. Adapters' IsByz returns false for all
//     ops because no operator is "byzantine" — the silence is a network /
//     leader-availability failure mode, not a byzantine action.
//   - Equivocation patterns: ByzOperators[0] is the leader-equivocator;
//     additional byz operators (if any) cooperate per pattern semantics.
type ByzPattern struct {
	Kind         ByzKind
	ByzOperators []OperatorID

	// Recipients carries operator IDs for selective-delivery patterns.
	// Conventions (length scales with f at the catalog scenario's apply time):
	//   - ByzEquivocateSigmaLockedSplit: first half → V_a recipients, second
	//     half → V_b recipients (size 2f total). Default at f=1 / n=4: {op2}
	//     → V_a, {op3} → V_b.
	//   - ByzPartialEquivocation: all-but-last → V_a recipients (size 2f),
	//     last → V_b recipient (size 1). Total 2f+1. Default at f=1 / n=4:
	//     {op2, op3} → V_a, {op4} → V_b.
	//   - ByzHV1SelectiveDelivery: full list of f honest that receive V (size
	//     f). Default at f=1 / n=4: {op2}.
	Recipients []OperatorID

	// Crashed lists operators that are completely offline for the slot.
	// A crashed op emits nothing in any role (no leader broadcast, no
	// σ/NR commit, no certificate gossip) and neither sends nor receives
	// on the wire; every adapter reports it as Decided=false / Err="offline"
	// and excludes it from the mesh topology entirely. Orthogonal to Kind,
	// so a crash composes with any byzantine pattern — the byz behavior
	// applies to ByzOperators, the crash to Crashed. The two sets MUST be
	// disjoint and satisfy len(ByzOperators)+len(Crashed) ≤ f (enforced by
	// Validate). Crashes are a pure liveness fault: a fully-silent op can
	// never help a byzantine aggregator forge a second V, so the safety
	// invariants are unaffected.
	Crashed []OperatorID

	// CrashedCount, when > 0 and Crashed is empty, asks Validate to draw a
	// random Crashed set of this size from Seed (excluding ByzOperators).
	// Used by the Healthy "faulty_nodes" knob, which varies the COUNT and
	// lets each seed pick a different victim set, so a deep-sampled cell
	// averages over crash positions (leader and non-leader alike). Resolved
	// to concrete IDs in Validate; adapters only ever read Crashed.
	CrashedCount int

	K     int // for ByzMultiSilent: how many top leaders are silent
	Layer int // for layer-targeted patterns (e.g. ByzFakeEncryptedPresence)
}

// PrimaryByz returns the first ByzOperators entry, or 0 if empty.
// Convenience for patterns that target a single byz; complete patterns iterate
// ByzOperators directly.
func (p ByzPattern) PrimaryByz() OperatorID {
	if len(p.ByzOperators) == 0 {
		return 0
	}
	return p.ByzOperators[0]
}

// IsByz reports whether op is in ByzOperators.
func (p ByzPattern) IsByz(op OperatorID) bool {
	for _, b := range p.ByzOperators {
		if b == op {
			return true
		}
	}
	return false
}

// IsCrashed reports whether op is in Crashed (a completely-offline operator).
func (p ByzPattern) IsCrashed(op OperatorID) bool {
	for _, c := range p.Crashed {
		if c == op {
			return true
		}
	}
	return false
}

// PickRecipient returns Recipients[idx] if present, else fallback. Used by
// per-protocol byz translators to read positional Recipients entries with a
// stable default at f=1 / n=4 (typical defaults: 2, 3, 4).
func (p ByzPattern) PickRecipient(idx int, fallback OperatorID) OperatorID {
	if idx < len(p.Recipients) {
		return p.Recipients[idx]
	}
	return fallback
}

// ByzKind enumerates the abstract byz behaviors. Add new kinds at the end
// to keep numeric values stable. Per-kind protocol behavior is documented in
// the catalog scenario notes.
type ByzKind int

const (
	ByzNone                       ByzKind = iota // all-honest baseline
	ByzSilentLeader                              // primary leader suppresses their candidate
	ByzMultiSilent                               // top K leaders silent (set ByzPattern.K)
	ByzEquivocate111                             // primary delivers a distinct V to each honest
	ByzEquivocateAllNR                           // primary floods both V's to all honest
	ByzEquivocateSigmaLockedSplit                // primary delivers V_a to one, V_b to another, ∅ to rest
	ByzHV1SelectiveDelivery                      // OBFT-specific: V to exactly one honest near horizon
	ByzFakeEncryptedPresence                     // OBFT-specific: silent at L_0, garbage bytes at L_k>0
	ByzSigmaRefusal                              // byz never contributes σ / never NRs

	// Added in Phase 3 (new byz patterns):
	ByzCrossSigning           // (Rule 1) byz emits both σ at L_k and NR on nr_tag_k
	ByzCrossOnionEquivocation // (Rule 3) byz emits two distinct σ partials on different V's at same layer
	ByzFakePlaintextSigma     // (Rule 5) byz emits plaintext σ at L_0 on a V they never broadcast
	ByzLateLeaderBroadcast    // leader broadcasts past T_broadcast_max_k (asymmetric absorption test)
	ByzWithholdLeader         // byz silent at the deepest layer specifically
	ByzAggregatorBypass       // (negative) byz forges identities to trigger NoOfflineDoubleV; do NOT add to Catalog (matrix tests would crash on safety panic)
	ByzPartialEquivocation    // natural-recovery equivocation: byz delivers V to 2f honest, V' to 1 honest; σ-pool reaches qV on V (OBFT.md:443)
	ByzCertWithholding        // byz reconstructs cert but doesn't gossip

	// The next three enum values are RESERVED for symmetry with the deferred-
	// byz catalog list, but are covered at other layers — adapters return
	// ErrNotApplicable for them rather than synthesizing redundant scenarios.

	ByzGarbageMessages       // RESERVED: covered by obft.ValidateCommit/ValidatePhase1Bundle direct unit tests; ByzFakeEncryptedPresence exercises a specific garbage variant (forged IBE ciphertext → Rule 4)
	ByzExceedsRateLimit      // RESERVED: covered by obft.Instance peerCommitHashes cap (MaxCommitHashesPerOp=8) directly in instance_test.go; validation-layer mirror in message/validation/obft_admissions_test.go
	ByzOfflineDoubleVAttempt // RESERVED: the OfflineAggregator runs on EVERY scenario via recordCommitToAggregator + AttemptAll; NoOfflineDoubleV is a universal safety invariant. ByzAggregatorBypass is the active-attack variant (forged identities)

	ByzWitnessForgery // (negative) byz forges Witnesses[] entries crediting honest leaders with σ on V_prime at deeper layers; sibling to ByzAggregatorBypass exercising the recordCommitToAggregator Witnesses path. do NOT add to Catalog

	ByzDelayedCommit // byz operator emits Phase-2 KindCommit on time per protocol state but with extra dispatch delay so arrival lands past RoundEndOffset. Exercises OBFT.md §Phase 3 "Re-running on late KindCommit arrivals" recovery path (always-on in the framework). Per-op behavior is otherwise honest (Phase-1 broadcasts on time, host-validates V normally).

	// 2abOBFT-specific: Phase-2 equivocation (Rule 6a) at the receiver-side.
	// A non-leader byz operator emits its natural Phase-2a KindValue on V
	// PLUS an extra KindValue on a distinct V_b ≠ V. Receivers fire Rule 6a
	// (Phase2Equivocation) on the cross-V second emission; the cluster's
	// honest majority still reaches σ-quorum on V at L_0. Other protocols
	// have no analog (OBFT has no Phase-2a coordination layer; QBFT's
	// PREPARE pool runs a different equivocation-detection scheme; PSigs
	// has no leader broadcast). All non-twoab adapters return
	// ErrNotApplicable for this kind.
	ByzPhase2EquivocateCrossV

	// 2abOBFT-specific: Phase-2 "downgrade" equivocation (Rule 6a). A byz
	// operator emits its natural KindValue on V at Phase 2a AND ALSO emits
	// a KindNoValue from the same op. The sequence "KindValue → KindNoValue"
	// is not in the authorized A1-A8 set per spec — downgrade is unauthorized
	// (only the reverse direction, KindNoValue → KindValue upgrade, is A1).
	// Receivers fire Rule 6a on the cross-kind second observation. Cluster
	// still decides via honest majority at L_0. Exercises the cross-kind
	// BuildExtra hook path in the twoab adapter; other protocols have no
	// analog. All non-twoab adapters return ErrNotApplicable.
	ByzPhase2DowngradeValueNoValue
)

// String returns a stable human-readable name for telemetry.
func (k ByzKind) String() string {
	switch k {
	case ByzNone:
		return "None"
	case ByzSilentLeader:
		return "SilentLeader"
	case ByzMultiSilent:
		return "MultiSilent"
	case ByzEquivocate111:
		return "Equivocate111"
	case ByzEquivocateAllNR:
		return "EquivocateAllNR"
	case ByzEquivocateSigmaLockedSplit:
		return "EquivocateSigmaLockedSplit"
	case ByzHV1SelectiveDelivery:
		return "HV1SelectiveDelivery"
	case ByzFakeEncryptedPresence:
		return "FakeEncryptedPresence"
	case ByzSigmaRefusal:
		return "SigmaRefusal"
	case ByzCrossSigning:
		return "CrossSigning"
	case ByzCrossOnionEquivocation:
		return "CrossOnionEquivocation"
	case ByzFakePlaintextSigma:
		return "FakePlaintextSigma"
	case ByzLateLeaderBroadcast:
		return "LateLeaderBroadcast"
	case ByzWithholdLeader:
		return "WithholdLeader"
	case ByzAggregatorBypass:
		return "AggregatorBypass"
	case ByzPartialEquivocation:
		return "PartialEquivocation"
	case ByzCertWithholding:
		return "CertWithholding"
	case ByzGarbageMessages:
		return "GarbageMessages"
	case ByzExceedsRateLimit:
		return "ExceedsRateLimit"
	case ByzOfflineDoubleVAttempt:
		return "OfflineDoubleVAttempt"
	case ByzWitnessForgery:
		return "WitnessForgery"
	case ByzDelayedCommit:
		return "DelayedCommit"
	case ByzPhase2EquivocateCrossV:
		return "Phase2EquivocateCrossV"
	case ByzPhase2DowngradeValueNoValue:
		return "Phase2DowngradeValueNoValue"
	default:
		return "Unknown"
	}
}
