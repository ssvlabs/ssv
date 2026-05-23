package twoab

import (
	"errors"
	"fmt"
)

// Verifier holds the per-cluster constants needed to verify inner-message
// BLS / IBE-tag material at SSV's message-validation boundary, without
// instantiating a full *Instance. It is the 2abOBFT sibling of base.Verifier;
// the method set differs because 2abOBFT splits Phase 2 into the KindValue /
// KindNoValue coordination envelopes and carries the σ partial inline on
// KindValue (no separate σ-commit).
//
// Validation-time verification closes the DoS gap where a byzantine operator
// (with a valid outer RSA identity) submits Phase1Bundles / ValueMsgs /
// NoValueMsgs / Commits / Certificates with garbage σ / NR partials, forcing
// every honest receiver to do expensive BLS work in the consensus-critical
// path. Failed verification rejects the message at the validation layer before
// dispatch.
//
// What this verifies:
//   - Phase1Bundle LeaderSigma — the layer leader's V-keypair σ partial on V
//     (identical to base; Phase1Bundle is a shared obft type).
//   - ValueMsg L0Partial — the emitter's own σ partial on V at L_0 (a failed
//     verify is unambiguous emitter misbehavior; see the messages.go L0Partial
//     vs Witnesses attribution note).
//   - Commit L0Partial — the emitter's nr_tag_0 IBE partial at L_0.
//   - NRPlaintext LayerEntries (on ValueMsg / NoValueMsg / Commit-NRDirect) —
//     the emitter's own nr_tag_k IBE partials at L_k>0.
//   - Certificate aggregate sig — full V-keypair signature against the cluster
//     pubkey (identical to base; Certificate is a shared obft type).
//
// What this does NOT verify (kept at the protocol layer to preserve evidence
// semantics, or because the material isn't verifiable without per-instance
// state / keys):
//   - ValueMsg forwarded Witnesses — a failed witness verify is silently
//     dropped at the protocol layer (anti-framing: the emitter's envelope does
//     not cryptographically attribute the forwarded bytes to the named leader),
//     so rejecting the whole ValueMsg on a bad witness would be unsound. The
//     leader-signed-garbage attack stays covered by VerifyPhase1Bundle, where
//     the bundle's own envelope binds the bytes to the leader.
//   - SigmaChained LayerEntries — the σ partial is chained-IBE-encrypted under
//     nr_tag_0..nr_tag_{k-1}; opening it requires the NR-quorum chain key
//     derived during Phase 3 reconstruction (per-instance state).
//
// All Verifier methods are read-only / concurrency-safe; the underlying Signers
// are verify-only (constructed without a share).
//
// Option A vs Option B (NR-side keypair) follows base.Verifier exactly:
// NRPubKeyShares == nil reuses the V-keypair shares for NR verification (the
// SSV production setting, with DST separation in the TagSigner); a populated
// NRPubKeyShares is the separate-IBE-keypair (Option B) deployment.
type Verifier struct {
	Signer         Signer // V-keypair signer (verify-only)
	TagSigner      Signer // IBE-keypair signer (verify-only); may equal Signer under Option A
	PubKeyShares   map[OperatorID][]byte
	NRPubKeyShares map[OperatorID][]byte // optional; nil → falls back to PubKeyShares (Option A)
	ClusterPubKey  []byte
}

// VerifyPhase1Bundle checks LeaderSigma against the bundle's claimed
// OperatorID's pub-share on V. Identical to base.VerifyPhase1Bundle (shared
// obft.Phase1Bundle type). Does not enforce structural invariants — pair with
// ValidatePhase1Bundle for that.
func (v *Verifier) VerifyPhase1Bundle(b *Phase1Bundle) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if b == nil {
		return errors.New("twoab: nil phase-1 bundle")
	}
	share, ok := v.PubKeyShares[b.OperatorID]
	if !ok || len(share) == 0 {
		return fmt.Errorf("twoab: no pub-key share for operator %d", b.OperatorID)
	}
	if !v.Signer.VerifyPartial(share, b.Value, b.LeaderSigma) {
		return fmt.Errorf("twoab: phase-1 σ_V from op %d does not verify", b.OperatorID)
	}
	return nil
}

// VerifyValueMsg checks the emitter's own σ partial (L0Partial) against its
// pub-share on V, plus any NRPlaintext LayerEntries (nr_tag_k partials at
// L_k>0). The forwarded Witnesses are intentionally NOT verified here (see the
// type doc — anti-framing). Does not enforce structural invariants — pair with
// ValidateValueMsg for that.
func (v *Verifier) VerifyValueMsg(m *ValueMsg) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if m == nil {
		return errors.New("twoab: nil value msg")
	}
	share, ok := v.PubKeyShares[m.OperatorID]
	if !ok || len(share) == 0 {
		return fmt.Errorf("twoab: no pub-key share for operator %d", m.OperatorID)
	}
	if !v.Signer.VerifyPartial(share, m.V, m.L0Partial) {
		return fmt.Errorf("twoab: ValueMsg L_0 σ partial from op %d does not verify", m.OperatorID)
	}
	return v.verifyNRLayerEntries(m.OperatorID, m.ClusterID, m.Height, m.LayerEntries)
}

// VerifyNoValueMsg checks the NRPlaintext LayerEntries (nr_tag_k partials).
// A NoValueMsg carries no L_0 payload, so there is nothing else to verify at
// L_0. Does not enforce structural invariants — pair with ValidateNoValueMsg.
func (v *Verifier) VerifyNoValueMsg(m *NoValueMsg) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if m == nil {
		return errors.New("twoab: nil no-value msg")
	}
	return v.verifyNRLayerEntries(m.OperatorID, m.ClusterID, m.Height, m.LayerEntries)
}

// VerifyCommit checks the emitter's nr_tag_0 IBE partial (L0Partial) at L_0,
// plus any NRPlaintext LayerEntries (carried only by Side=NRDirect). All valid
// Commit sides are NR-direction. Failure here is structural malformedness (the
// operator signed a bad NR partial) and rejects the whole Commit; it fires no
// Rule evidence — pure malformedness, not equivocation. Does not enforce
// structural invariants (Side validity, layer bounds) — pair with
// ValidateCommit.
func (v *Verifier) VerifyCommit(c *Commit) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if c == nil {
		return errors.New("twoab: nil commit")
	}
	nrShares := v.nrShares()
	share, ok := nrShares[c.OperatorID]
	if !ok || len(share) == 0 {
		return fmt.Errorf("twoab: no NR pub-key share for operator %d", c.OperatorID)
	}
	tag := NoQuorumTag(c.ClusterID, c.Height, 0)
	if !v.TagSigner.VerifyPartial(share, tag, c.L0Partial) {
		return fmt.Errorf("twoab: commit L_0 nr_tag partial from op %d does not verify", c.OperatorID)
	}
	return v.verifyNRLayerEntries(c.OperatorID, c.ClusterID, c.Height, c.LayerEntries)
}

// VerifyCertificate checks the aggregate sig on c against ClusterPubKey.
// Identical to base.VerifyCertificate (shared obft.Certificate type).
func (v *Verifier) VerifyCertificate(c *Certificate) error {
	if err := v.checkConfigured(); err != nil {
		return err
	}
	if c == nil {
		return errors.New("twoab: nil certificate")
	}
	if !v.Signer.VerifyAggregate(v.ClusterPubKey, c.Value, c.Signature) {
		return errors.New("twoab: certificate signature does not verify against cluster pubkey")
	}
	return nil
}

// verifyNRLayerEntries verifies each NRPlaintext entry's nr_tag_k IBE partial
// against the emitter's NR pub-share. Empty / SigmaChained entries are skipped
// (nothing the validation layer can verify without per-instance keys). The NR
// share is looked up once, only when at least one NRPlaintext entry is present,
// so a message with no NR commitments needs no NR share. Mirrors the protocol
// layer's verifyNRTagPartial gate.
func (v *Verifier) verifyNRLayerEntries(op OperatorID, clusterID [32]byte, height Height, entries []LayerEntry) error {
	var share []byte
	var resolved bool
	for idx, e := range entries {
		if e.Kind != LayerEntryNRPlaintext {
			continue
		}
		if !resolved {
			s, ok := v.nrShares()[op]
			if !ok || len(s) == 0 {
				return fmt.Errorf("twoab: no NR pub-key share for operator %d", op)
			}
			share, resolved = s, true
		}
		tag := NoQuorumTag(clusterID, height, e.Layer)
		if !v.TagSigner.VerifyPartial(share, tag, Signature(e.Payload)) {
			return fmt.Errorf("twoab: NR partial (entry %d, layer %d) from op %d does not verify", idx, e.Layer, op)
		}
	}
	return nil
}

// nrShares returns the NR-side pub-key shares: NRPubKeyShares under Option B,
// or PubKeyShares under Option A (nil NRPubKeyShares).
func (v *Verifier) nrShares() map[OperatorID][]byte {
	if v.NRPubKeyShares != nil {
		return v.NRPubKeyShares
	}
	return v.PubKeyShares
}

// checkConfigured returns an error if the Verifier is missing required
// dependencies (nil receiver, nil Signer, nil TagSigner). Callers may also set
// NRPubKeyShares explicitly under Option B; nil falls back to PubKeyShares.
func (v *Verifier) checkConfigured() error {
	if v == nil {
		return errors.New("twoab: nil verifier")
	}
	if v.Signer == nil {
		return errors.New("twoab: verifier has nil Signer")
	}
	if v.TagSigner == nil {
		return errors.New("twoab: verifier has nil TagSigner")
	}
	return nil
}
