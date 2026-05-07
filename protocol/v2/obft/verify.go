package obft

import (
	"errors"
	"fmt"
)

// Verifier holds the per-cluster constants needed to verify inner-message
// BLS / IBE-tag material at SSV's message-validation boundary, without
// instantiating a full *Instance.
//
// Validation-time verification closes the DoS gap where a byzantine operator
// (with a valid outer RSA identity) submits Phase1Bundles / Commits /
// Certificates with garbage σ_V or NR partials, forcing every honest receiver
// to do expensive BLS work in the consensus-critical path. Failed verification
// rejects the message at the validation layer before dispatch.
//
// What this verifies:
//   - Phase1Bundle σ_V — the leader's V-keypair partial sig.
//   - Commit NR partials — operator's IBE-keypair partial sigs on nr_tag_k.
//   - Certificate aggregate sig — full V-keypair signature against the
//     cluster pubkey.
//
// What this does NOT verify (kept at protocol layer to preserve evidence
// semantics):
//   - Commit L_0 σ entries — Rule 5 evidence requires checking against
//     retained V's, which is per-instance state.
//   - Commit Witnesses — Rule 2 evidence on distinct V's at same (layer,
//     leader) requires per-instance retention bookkeeping.
//   - Commit deeper-layer σ entries — IBE-encrypted; requires NR-quorum
//     chain key derived during Phase 3 reconstruction.
//
// All Verifier methods are read-only / concurrency-safe; the underlying
// Signers are verify-only (constructed without a share).
//
// Option A vs Option B (NR-side keypair):
//
//   - Option A (NRPubKeyShares == nil): the V-keypair shares double as the
//     IBE-keypair shares, with domain separation between V-signing and
//     NR-tag-signing handled by the TagSigner's distinct DST. BLS pubkeys
//     are domain-agnostic, so verifying a TagSigner partial against a
//     V-keypair public share works correctly — the DST is applied during
//     the partial's construction, not the pubkey's. This is the SSV
//     production setting.
//   - Option B (NRPubKeyShares populated): a separate IBE-keypair has its
//     own per-operator shares; pass them explicitly. The V-shares are not
//     reused for NR verification.
type Verifier struct {
	Signer         Signer // V-keypair signer (verify-only)
	TagSigner      Signer // IBE-keypair signer (verify-only); may equal Signer under Option A
	PubKeyShares   map[OperatorID][]byte
	NRPubKeyShares map[OperatorID][]byte // optional; nil → falls back to PubKeyShares (Option A)
	ClusterPubKey  []byte
}

// VerifyPhase1Bundle checks σ_V against the bundle's claimed OperatorID's
// pub-share. Does not enforce structural invariants (height, layer bounds,
// leader-claim) — pair with ValidatePhase1Bundle for that.
func (v *Verifier) VerifyPhase1Bundle(b *Phase1Bundle) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if b == nil {
		return errors.New("obft: nil phase-1 bundle")
	}
	share, ok := v.PubKeyShares[b.OperatorID]
	if !ok || len(share) == 0 {
		return fmt.Errorf("obft: no pub-key share for operator %d", b.OperatorID)
	}
	if !v.Signer.VerifyPartial(share, b.Value, b.SigmaV) {
		return fmt.Errorf("obft: phase-1 σ_V from op %d does not verify", b.OperatorID)
	}
	return nil
}

// VerifyCommitNRPartials checks each NR partial in c against its claimed
// signer's IBE pub-share. One verification per NR layer (≤ K-1 total).
//
// Failure here is structurally invalid (the operator signed a malformed NR
// partial) and rejects the whole Commit. Does not fire any Rule evidence —
// pure malformedness, not equivocation.
func (v *Verifier) VerifyCommitNRPartials(c *Commit) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if c == nil {
		return errors.New("obft: nil commit")
	}
	if len(c.NRPartials) == 0 {
		return nil
	}
	nrShares := v.NRPubKeyShares
	if nrShares == nil {
		nrShares = v.PubKeyShares
	}
	share, ok := nrShares[c.OperatorID]
	if !ok || len(share) == 0 {
		return fmt.Errorf("obft: no NR pub-key share for operator %d", c.OperatorID)
	}
	for _, p := range c.NRPartials {
		tag := NoQuorumTag(c.ClusterID, c.Height, p.Layer)
		if !v.TagSigner.VerifyPartial(share, tag, p.PartialSig) {
			return fmt.Errorf("obft: NR partial from op %d at layer %d does not verify",
				c.OperatorID, p.Layer)
		}
	}
	return nil
}

// VerifyCommitWitnesses checks every witness's σ_V against its claimed
// leader's V-share. Defense-in-depth on top of protocol-layer rehydration:
// if a byzantine packs garbage witnesses to force expensive BLS verification
// on every receiver, this rejects at the validation boundary.
//
// Rule 2 (leader equivocation) detection is unaffected — that fires at the
// protocol layer when a leader's bundle/witness retention reaches 2 distinct
// V's; here we only reject witnesses whose σ_V doesn't verify, which is a
// malformedness check, not equivocation.
func (v *Verifier) VerifyCommitWitnesses(c *Commit) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if c == nil {
		return errors.New("obft: nil commit")
	}
	for i, w := range c.Witnesses {
		share, ok := v.PubKeyShares[w.Leader]
		if !ok || len(share) == 0 {
			return fmt.Errorf("obft: witness %d: no pub-key share for leader %d", i, w.Leader)
		}
		if !v.Signer.VerifyPartial(share, w.Value, w.SigmaV) {
			return fmt.Errorf("obft: witness %d (leader %d, layer %d) σ_V does not verify",
				i, w.Leader, w.Layer)
		}
	}
	return nil
}

// VerifyCertificate checks the aggregate sig on c against ClusterPubKey.
// One verification per certificate.
func (v *Verifier) VerifyCertificate(c *Certificate) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if c == nil {
		return errors.New("obft: nil certificate")
	}
	if !v.Signer.VerifyAggregate(v.ClusterPubKey, c.Value, c.Signature) {
		return errors.New("obft: certificate signature does not verify against cluster pubkey")
	}
	return nil
}

// checkConfigured returns an error if the Verifier is missing required
// dependencies (nil receiver, nil Signer, nil TagSigner). Callers may also
// want to set NRPubKeyShares explicitly under Option B; nil falls back to
// PubKeyShares (Option A).
func (v *Verifier) checkConfigured() error {
	if v == nil {
		return errors.New("obft: nil verifier")
	}
	if v.Signer == nil {
		return errors.New("obft: verifier has nil Signer")
	}
	if v.TagSigner == nil {
		return errors.New("obft: verifier has nil TagSigner")
	}
	return nil
}
