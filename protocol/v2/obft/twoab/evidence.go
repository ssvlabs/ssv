package twoab

// Slashing-evidence types. Per spec §Slashing evidence, 2abOBFT surfaces
// six rules of byzantine-fault evidence: Rules 1-5 inherited from bare OBFT
// (with slight wire-shape adjustments for 2abOBFT's message kinds) and
// Rule 6a for Phase-2 equivocation. (Rule 6b "verdict-vs-action" from the
// previous 2abOBFT design was dropped: KindValue is coordination-only — no
// threshold partial on the wire — so the pivot KindValue → KindCommit-NR is
// legitimate when authorized per §Authorized Phase-2 emission pairs A3/A4.)
//
// None of these rules is load-bearing for safety; they exist so the
// surviving operators can blacklist misbehaving operators (planned
// protocol extension) and so stakers can migrate validators away from
// underperforming clusters.
//
// The protocol layer surfaces evidence as inner-message contradictions;
// the adapter layer pairs each piece of evidence with the SignedSSVMessage
// envelopes that authenticate it. Per spec §Slashing evidence, honest
// operators MUST log observed evidence per-rule for out-of-band
// aggregation. There is no dedicated on-wire evidence gossip — underlying
// signed messages (bundles, ValueMsg/NoValueMsg/Commit, certificates)
// already propagate via normal protocol message flow.

// EvidenceRule names the six rules from spec §Slashing evidence.
type EvidenceRule int

const (
	// EvidenceCrossSigning — Rule 1: an operator emitted both σ_i^V at a
	// layer AND σ_i^IBE(nr_tag_k) at the same layer. Cross-phase
	// exclusivity is EKM-enforced, so a dual emission is byzantine.
	EvidenceCrossSigning EvidenceRule = 1

	// EvidenceLeaderEquivocation — Rule 2: a layer's leader emitted two
	// distinct Phase-1 bundles on different value_roots at the same
	// (slot, layer). Self-contained slashable proof (bundles are op-
	// identity-signed at the envelope layer; the pair is unambiguous).
	EvidenceLeaderEquivocation EvidenceRule = 2

	// EvidenceCrossCommitEquivocation — Rule 3: an operator emitted σ_i^V
	// on V and σ_i^V on V' at the same layer. Post Op5 at L_0, this fires
	// when two distinct ValueMsgs from the same op carry verifying
	// L0Partials on different V's (the σ partials moved from KindCommit-
	// Signed into KindValue.L0Partial). At L_k>0 the detection path is
	// unchanged (chained σ partials inside LayerEntries). Single-σ-V
	// exclusivity is EKM-enforced; observing two valid partials on
	// distinct V's is cryptographic proof.
	EvidenceCrossCommitEquivocation EvidenceRule = 3

	// EvidenceFakeEncryptedPresence — Rule 4: at layer k > 0, an
	// operator's Phase-2a LayerEntry (carried inside ValueMsg / NoValueMsg
	// / Commit-NRDirect) decrypts (post-NR-quorum at prior layers) to
	// garbage rather than a valid σ partial. Detection is delayed and
	// conditional on slot progression unlocking the layer's chained
	// encryption.
	EvidenceFakeEncryptedPresence EvidenceRule = 4

	// EvidenceFakePlaintextSigma — Rule 5: at L_0, an operator's plaintext
	// σ partial does not verify against the operator's pubKeyShare on
	// the claimed V. Detection points (post Op5):
	//   - ValueMsg.L0Partial verify-fail → Rule 5 keyed on the EMITTER
	//     (the L0Partial is the emitter's own signing artifact).
	//   - Phase1Bundle.L0Witness verify-fail → Rule 5 keyed on the
	//     LEADER (the bundle is op-identity-signed by the leader at the
	//     outer envelope).
	//   - ValueMsg.L0Witness verify-fail does NOT fire Rule 5 (anti-
	//     framing: the L0Witness inside KindValue is signed-for-
	//     forwarding by the emitter, not the leader — firing Rule 5
	//     against the leader on a peer-emitter's forged witness would
	//     open a framing attack).
	EvidenceFakePlaintextSigma EvidenceRule = 5

	// EvidencePhase2Equivocation — Rule 6a (2abOBFT-specific): an operator
	// emitted a Phase-2 sequence not in the authorized set (see §Authorized
	// Phase-2 emission pairs). Cryptographic, self-contained — all
	// envelopes are op-identity-signed by the offender; the offending
	// sequence is unambiguous from a single observer's view.
	//
	// Authorized (NOT slashable) post Op5 — set shrunk from 8 to 3:
	//   A1: KindNoValue → KindValue (upgrade — KindValue carries σ partial)
	//   A5: KindNoValue → KindCommit-NR
	//   A8: KindCommit-NRDirect (alone — Phase-2a NRDirect on equivocation)
	//
	// A2/A3/A4/A6/A7 are OBSOLETE post Op5 — KindCommit-Signed no longer
	// exists, σ partial moved into KindValue itself, and the σ-locked-
	// on-emit semantics preclude A3/A4 pivots.
	//
	// Slashable (NOT in A1/A5/A8):
	//   - Two KindValue on different V_0 (also Rule 3 cross-σ-V if both
	//     L0Partials verify)
	//   - KindValue → KindNoValue (downgrade)
	//   - KindValue → KindCommit-NR / KindCommit-NRDirect (σ-XOR-NR
	//     violation; also Rule 1 if partials verify)
	//   - KindNoValue → KindCommit-NRDirect (A8 sole-emission violation)
	//   - KindCommit-NRDirect followed by any other emission
	//   - KindNoValue → KindCommit-NR → KindValue (post-NR-commit σ-claim)
	//   - Any other 3+ message sequence not matching A1 or A5
	EvidencePhase2Equivocation EvidenceRule = 6
)

// String returns the rule name for telemetry/logging.
func (r EvidenceRule) String() string {
	switch r {
	case EvidenceCrossSigning:
		return "cross-signing"
	case EvidenceLeaderEquivocation:
		return "leader-equivocation"
	case EvidenceCrossCommitEquivocation:
		return "cross-commit-equivocation"
	case EvidenceFakeEncryptedPresence:
		return "fake-encrypted-presence"
	case EvidenceFakePlaintextSigma:
		return "fake-plaintext-sigma-at-L0"
	case EvidencePhase2Equivocation:
		return "phase2-equivocation"
	default:
		return "unknown"
	}
}

// Evidence is a discriminated union of the six evidence types. Exactly
// one of the typed payload fields is set, matching Rule.
type Evidence struct {
	Rule       EvidenceRule
	OperatorID OperatorID
	Layer      int

	// Per-rule payloads. Only one is populated.

	CrossSigning            *CrossSigningEvidence
	LeaderEquivocation      *LeaderEquivocationEvidence
	CrossCommitEquivocation *CrossCommitEquivocationEvidence
	FakeEncryptedPresence   *FakeEncryptedPresenceEvidence
	FakePlaintextSigma      *FakePlaintextSigmaEvidence
	Phase2Equivocation      *Phase2EquivocationEvidence
}

// CrossSigningEvidence (Rule 1) — Operator OperatorID emitted both σ at
// Layer and NR at Layer (across two distinct Phase-2 emissions).
type CrossSigningEvidence struct {
	SigmaPartial Signature
	SigmaValue   Value
	NRPartial    Signature
}

// LeaderEquivocationEvidence (Rule 2) — Two distinct Phase-1 bundles
// from the same leader at the same (slot, layer).
type LeaderEquivocationEvidence struct {
	BundleA *Phase1Bundle
	BundleB *Phase1Bundle
}

// CrossCommitEquivocationEvidence (Rule 3) — Operator OperatorID has σ
// partials on two distinct V's at the same layer. The two partials may
// come from any combination of:
//   - L_0: Commit-Signed (the L_0 σ partial)
//   - L_k>0: ValueMsg / NoValueMsg / Commit-NRDirect LayerEntries with
//     Kind=SigmaChained
type CrossCommitEquivocationEvidence struct {
	ValueA   Value
	ValueB   Value
	PartialA Signature
	PartialB Signature
}

// FakeEncryptedPresenceEvidence (Rule 4) — Operator OperatorID's Phase-2a
// LayerEntry at Layer (k > 0) decrypted to garbage rather than a valid
// σ partial. `Ciphertext` is the offending entry's payload;
// `DecryptedBytes` is what it produced; `DecryptError` is set if
// decryption itself failed.
type FakeEncryptedPresenceEvidence struct {
	Ciphertext     []byte
	DecryptedBytes []byte
	DecryptError   string
}

// FakePlaintextSigmaEvidence (Rule 5) — Operator OperatorID's Commit-Signed
// at L_0 carries a plaintext σ partial that doesn't verify against any
// retained Phase-1 V at L_0.
type FakePlaintextSigmaEvidence struct {
	OnionPartial Signature
	OnionValue   Value
	// RetainedValueHashes lists the value_roots the receiver had
	// retained at L_0 at detection time.
	RetainedValueHashes [][]byte
}

// Phase2EquivocationEvidence (Rule 6a) — Operator OperatorID emitted a
// Phase-2 sequence not in the authorized A1-A8 set. The evidence carries
// the offending message pair; receivers MAY act on a single observed
// pair (cluster-wide consensus on the evidence is not required).
//
// Exactly one of {ValueA, NoValueA, CommitA} is set (the first observed
// Phase-2 emission from the op); similarly for {ValueB, NoValueB,
// CommitB} (the offending second emission). The detection paths in
// ObserveValueMsg / ObserveNoValueMsg / ObserveCommit populate these.
//
// Triple-message sequences (e.g. KindNoValue → KindCommit-NR → KindValue
// listed as slashable in the redesign plan) are NOT detected here:
// from a receiver's observation set alone, that triple is
// indistinguishable from the authorized A7 sequence (KindNoValue →
// KindValue → KindCommit-NR) — both produce the same set of observed
// messages, and gossipsub doesn't provide wire-level emission-order
// metadata. Per §Receiver ordering tolerance, the receiver defaults to
// the authorized interpretation; the slashable variant is unenforceable
// from a single observer's view. Honest ops are gated against producing
// the triple at the build path (MaybeBuildAndBroadcastUpgrade rejects
// once ownCommit is set), so this affects only byz behavior — and
// receivers conservatively accept rather than slash on ambiguous
// evidence.
type Phase2EquivocationEvidence struct {
	ValueA   *ValueMsg
	NoValueA *NoValueMsg
	CommitA  *Commit

	ValueB   *ValueMsg
	NoValueB *NoValueMsg
	CommitB  *Commit
}

// EvidenceObserver fires on the FIRST recording per (Rule, OperatorID,
// Layer) tuple. Set at NewInstance construction; nil disables. Per spec
// §Slashing evidence, honest operators MUST log observed evidence per-
// rule for out-of-band aggregation; this callback is the logging
// surface — the SSV runner wires it to its preferred logger.
//
// The callback runs synchronously inside the protocol layer's recording
// path. No concurrency contract for callers — Instance is not
// thread-safe; the SSV adapter serializes access.
type EvidenceObserver func(Evidence)

// evidenceObservedKey is the dedup key for the observer firing. A given
// (Rule, OperatorID, Layer) tuple fires the observer at most once per
// Instance — even if the protocol records multiple Evidence entries for
// the same logical fault (e.g., redundant detection paths).
type evidenceObservedKey struct {
	rule  EvidenceRule
	op    OperatorID
	layer int
}
