package obft

// Slashing-evidence types. Per spec §Slashing evidence, OBFT surfaces five
// rules of byzantine-fault evidence for attribution and out-of-band punishment.
// None is load-bearing for safety (Pigeonholes 1-3 give cryptographic safety
// against any byzantine within the f-bound regardless of which evidence the
// honest operators choose to act on); they exist so the surviving operators
// can blacklist misbehaving operators (planned protocol extension restoring
// `Byzantine ≡ Down`) and so stakers can migrate validators away from
// underperforming clusters.
//
// The protocol layer surfaces evidence as inner-message contradictions; the
// adapter layer pairs each piece of evidence with the SignedSSVMessage
// envelopes that authenticate it (the outer SSV signature binds each inner
// message to its sender's identity). For Rule 5 specifically, the spec
// requires honest receivers to gossip the evidence (rate-limited per
// (slot, layer, operator_id)) so receivers without retained V can also
// attribute the fault.

// EvidenceRule names the five rules from spec §Slashing evidence.
type EvidenceRule int

const (
	// EvidenceCrossSigning — Rule 1: an operator emitted both σ_i^V at a
	// layer AND σ_i^IBE(nr_tag_k) at the same layer. Cross-phase exclusivity
	// is EKM-enforced, so a dual emission is byzantine.
	EvidenceCrossSigning EvidenceRule = 1

	// EvidenceLeaderEquivocation — Rule 2: a layer's leader emitted two
	// distinct Phase-1 σ_V partials on different value_roots at the same
	// (slot, layer). Self-contained slashable proof.
	EvidenceLeaderEquivocation EvidenceRule = 2

	// EvidenceCrossOnionEquivocation — Rule 3: an operator emitted σ_i^V on
	// V and σ_i^V on V' at the same layer (across one or multiple Onions).
	// Single-σ-V exclusivity is EKM-enforced.
	EvidenceCrossOnionEquivocation EvidenceRule = 3

	// EvidenceFakeEncryptedPresence — Rule 4: at layer k > 0, an operator's
	// auth-signed Onion entry decrypts (post-NR-quorum at prior layers) to
	// garbage rather than a valid σ partial on a known V. Detection is
	// delayed and conditional on slot progression unlocking the layer's
	// chained encryption.
	EvidenceFakeEncryptedPresence EvidenceRule = 4

	// EvidenceFakePlaintextSigma — Rule 5: at L_0, an operator's auth-signed
	// Onion carries a plaintext σ partial that does not verify against any
	// retained leader-broadcast V. Detection is immediate at retained-V
	// receivers; the spec's MUST-gossip rule (rate-limited per
	// (slot, layer, operator_id)) lets no-V receivers also attribute.
	EvidenceFakePlaintextSigma EvidenceRule = 5
)

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
	default:
		return "unknown"
	}
}

// Evidence is a discriminated union of the five evidence types. Exactly one
// of the typed fields is set, matching Rule.
type Evidence struct {
	Rule       EvidenceRule
	OperatorID OperatorID
	Layer      int

	// Per-rule payloads. Only one is populated.

	CrossSigning             *CrossSigningEvidence
	LeaderEquivocation       *LeaderEquivocationEvidence
	CrossOnionEquivocation   *CrossOnionEquivocationEvidence
	FakeEncryptedPresence    *FakeEncryptedPresenceEvidence
	FakePlaintextSigma       *FakePlaintextSigmaEvidence
}

// CrossSigningEvidence (Rule 1) — Operator OperatorID emitted both σ at Layer
// (in Onion or Phase-1 bundle for the layer's leader) AND NR at Layer.
type CrossSigningEvidence struct {
	SigmaPartial Signature // the σ partial seen
	SigmaValue   Value     // the V signed by SigmaPartial
	NRPartial    Signature // the NR partial seen
}

// LeaderEquivocationEvidence (Rule 2) — Two distinct Phase-1 bundles from
// the same leader at the same (slot, layer).
type LeaderEquivocationEvidence struct {
	BundleA *Phase1Bundle
	BundleB *Phase1Bundle
}

// CrossOnionEquivocationEvidence (Rule 3) — Operator OperatorID has σ partials
// on two distinct V's at the same layer.
type CrossOnionEquivocationEvidence struct {
	ValueA   Value
	ValueB   Value
	PartialA Signature
	PartialB Signature
}

// FakeEncryptedPresenceEvidence (Rule 4) — Operator OperatorID's Onion entry
// at Layer (k > 0) decrypted to garbage rather than a valid σ partial.
// `Ciphertext` is the offending entry's ciphertext; `DecryptedBytes` is what
// it produced after the chained-decryption walk; `DecryptError` is set if
// decryption itself failed.
type FakeEncryptedPresenceEvidence struct {
	Ciphertext     []byte
	DecryptedBytes []byte
	DecryptError   string
}

// FakePlaintextSigmaEvidence (Rule 5) — Operator OperatorID's L_0 Onion
// entry carries a plaintext σ partial that doesn't verify against any
// retained Phase-1 V at L_0.
type FakePlaintextSigmaEvidence struct {
	OnionPartial Signature
	OnionValue   Value // the value the Onion entry claimed σ on
	// RetainedValueHashes lists the value_roots the receiver had retained
	// at L_0 at detection time (helpful for third-party verification —
	// the verifier reproduces the partial-vs-V check).
	RetainedValueHashes [][]byte
}
