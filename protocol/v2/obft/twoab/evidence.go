package twoab

// Slashing-evidence types. Per spec §Slashing evidence, 2abOBFT surfaces
// seven rules of byzantine-fault evidence: Rules 1-5 inherited from bare
// OBFT and Rules 6a/6b new to 2ab's Phase-2a verdict surface. None is
// load-bearing for safety; they exist so the surviving operators can
// blacklist misbehaving operators (planned protocol extension) and so
// stakers can migrate validators away from underperforming clusters.
//
// The protocol layer surfaces evidence as inner-message contradictions;
// the adapter layer pairs each piece of evidence with the SignedSSVMessage
// envelopes that authenticate it. Per spec §Slashing evidence, honest
// operators MUST log observed evidence per-rule for out-of-band
// aggregation. There is no dedicated on-wire evidence gossip — underlying
// signed messages (bundles, verdicts, onions) already propagate via
// normal protocol message flow.

// EvidenceRule names the seven rules from spec §Slashing evidence.
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

	// EvidenceCrossOnionEquivocation — Rule 3: an operator emitted σ_i^V
	// on V and σ_i^V on V' at the same layer (across one or multiple
	// Onion2b messages). Single-σ-V exclusivity is EKM-enforced.
	EvidenceCrossOnionEquivocation EvidenceRule = 3

	// EvidenceFakeEncryptedPresence — Rule 4: at layer k > 0, an
	// operator's auth-signed Onion2b entry decrypts (post-NR-quorum at
	// prior layers) to garbage rather than a valid σ partial. Detection
	// is delayed and conditional on slot progression unlocking the
	// layer's chained encryption.
	EvidenceFakeEncryptedPresence EvidenceRule = 4

	// EvidenceFakePlaintextSigma — Rule 5: at L_0, an operator's auth-
	// signed Onion2b carries a plaintext σ partial that does not verify
	// against any retained leader-broadcast V. Detection requires the
	// receiver to have retained-or-auth-only-retained V at L_0.
	EvidenceFakePlaintextSigma EvidenceRule = 5

	// EvidenceVerdictEquivocation — Rule 6a (2ab-specific): an operator
	// broadcast two distinct KindVerdict envelopes for the same
	// (slot, layer). Cryptographic, self-contained — both envelopes are
	// op-identity-signed by the offender. Receivers MAY act on a single
	// observed pair; cluster-wide consensus on the evidence is not
	// required.
	EvidenceVerdictEquivocation EvidenceRule = 6

	// EvidenceVerdictAction — Rule 6b (2ab-specific): an operator
	// broadcast a verdict and then emitted a Phase-2b action that
	// contradicts it (e.g., σV verdict followed by NR partial emission).
	// Cryptographic but boundary-conditional — distinguishing honest
	// revision from byzantine equivocation requires cross-referencing
	// the cluster verdict view.
	EvidenceVerdictAction EvidenceRule = 7
)

// String returns the rule name for telemetry/logging.
func (r EvidenceRule) String() string {
	switch r {
	case EvidenceCrossSigning:
		return "cross-signing"
	case EvidenceLeaderEquivocation:
		return "leader-equivocation"
	case EvidenceCrossOnionEquivocation:
		return "cross-onion-equivocation"
	case EvidenceFakeEncryptedPresence:
		return "fake-encrypted-presence"
	case EvidenceFakePlaintextSigma:
		return "fake-plaintext-sigma-at-L0"
	case EvidenceVerdictEquivocation:
		return "verdict-equivocation"
	case EvidenceVerdictAction:
		return "verdict-vs-action"
	default:
		return "unknown"
	}
}

// Evidence is a discriminated union of the seven evidence types. Exactly
// one of the typed payload fields is set, matching Rule.
type Evidence struct {
	Rule       EvidenceRule
	OperatorID OperatorID
	Layer      int

	// Per-rule payloads. Only one is populated.

	CrossSigning           *CrossSigningEvidence
	LeaderEquivocation     *LeaderEquivocationEvidence
	CrossOnionEquivocation *CrossOnionEquivocationEvidence
	OnionEquivocation      *OnionEquivocationEvidence
	FakeEncryptedPresence  *FakeEncryptedPresenceEvidence
	FakePlaintextSigma     *FakePlaintextSigmaEvidence
	VerdictEquivocation    *VerdictEquivocationEvidence
	VerdictAction          *VerdictActionEvidence
}

// CrossSigningEvidence (Rule 1) — Operator OperatorID emitted both σ at
// Layer and NR at Layer.
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

// CrossOnionEquivocationEvidence (Rule 3, per-layer) — Operator
// OperatorID has σ partials on two distinct V's at the same layer.
type CrossOnionEquivocationEvidence struct {
	ValueA   Value
	ValueB   Value
	PartialA Signature
	PartialB Signature
}

// OnionEquivocationEvidence (Rule 3, top-level, Layer == -1) — Operator
// OperatorID emitted two structurally-distinct Onion2b messages at the
// same (slot). Carries the full Onion2b bodies so a third-party slashing
// verifier can recompute their content hashes and confirm the structural
// distinction.
type OnionEquivocationEvidence struct {
	OnionA *Onion2b
	OnionB *Onion2b
}

// FakeEncryptedPresenceEvidence (Rule 4) — Operator OperatorID's Onion2b
// entry at Layer (k > 0) decrypted to garbage rather than a valid σ
// partial. `Ciphertext` is the offending entry's ciphertext;
// `DecryptedBytes` is what it produced; `DecryptError` is set if
// decryption itself failed.
type FakeEncryptedPresenceEvidence struct {
	Ciphertext     []byte
	DecryptedBytes []byte
	DecryptError   string
}

// FakePlaintextSigmaEvidence (Rule 5) — Operator OperatorID's L_0
// Onion2b entry carries a plaintext σ partial that doesn't verify
// against any retained Phase-1 V at L_0.
type FakePlaintextSigmaEvidence struct {
	OnionPartial Signature
	OnionValue   Value
	// RetainedValueHashes lists the value_roots the receiver had
	// retained at L_0 at detection time.
	RetainedValueHashes [][]byte
}

// VerdictEquivocationEvidence (Rule 6a) — Operator OperatorID broadcast
// two distinct KindVerdict envelopes for the same (slot, layer). Self-
// contained cryptographic evidence — both envelopes are op-identity-
// signed by the offender; the pair is unambiguous from a single
// observer's view.
type VerdictEquivocationEvidence struct {
	VerdictA *Verdict
	VerdictB *Verdict
}

// VerdictActionEvidence (Rule 6b) — Operator OperatorID's broadcast
// verdict at (slot, layer) contradicts their Phase-2b action at the same
// layer.
//
// Higher false-positive risk than Rule 6a — honest revision (e.g., σV
// verdict at Phase-2a, then bundle-equivocation observed mid-Phase-2a,
// NR action at Phase-2b) is permitted. The distinguishing condition
// requires cross-referencing the cluster verdict view; receivers should
// log Rule-6b observations and aggregate out-of-band before acting.
type VerdictActionEvidence struct {
	Verdict      *Verdict
	ActionKind   EvidenceRule // either evidenceActionSigma or evidenceActionNR sentinel
	SigmaValue   Value        // populated when ActionKind = σ
	SigmaPartial Signature    // populated when ActionKind = σ
	NRPartial    Signature    // populated when ActionKind = NR
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
