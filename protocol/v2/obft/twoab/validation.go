package twoab

import (
	"errors"
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Structural validation for 2abOBFT messages received from peers.
//
// These are protocol-level shape checks: the message is well-formed for the
// instance's Config (right Height, right K, etc.). They do NOT:
//
//   - Verify the sender's identity (the SSV adapter has already verified
//     the outer envelope signature by the time the inner message reaches
//     the twoab core).
//   - Verify the inner partial-sig cryptography (per-partial verification
//     happens at observation time inside Instance methods, against the
//     pubKeyShares the Instance was constructed with).
//
// Standalone functions so the SSV adapter can validate before queuing or
// before deciding to accept the message at all.

// ValidatePhase1Bundle checks structural invariants for an incoming
// Phase-1 bundle. Per spec §Phase 1 (post Op3): the bundle carries the
// leader's σ partial on V as `L0Witness` at L_0 — required and non-empty
// when Layer==0. At L_k>0 (Phase D not shipped yet), L0Witness is empty.
// L0Witness BLS verification happens at the Instance layer
// (ObservePhase1Bundle) against the leader's pubKeyShare; only the
// non-empty-bytes structural check is here.
func ValidatePhase1Bundle(b *Phase1Bundle, cfg *Config) error {
	if b == nil {
		return errors.New("twoab: nil phase-1 bundle")
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if b.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: phase-1 bundle cluster id %x != instance cluster id %x",
			b.ClusterID, cfg.ClusterID)
	}
	if b.Height != cfg.Height {
		return fmt.Errorf("twoab: phase-1 bundle height %d != instance height %d", b.Height, cfg.Height)
	}
	if b.Layer < 0 || b.Layer >= cfg.K() {
		return fmt.Errorf("twoab: phase-1 bundle layer %d out of range [0, %d)", b.Layer, cfg.K())
	}
	expectedLeader := cfg.Layers[b.Layer].Leader
	if b.OperatorID != expectedLeader {
		return fmt.Errorf("twoab: phase-1 bundle from operator %d but layer %d's leader is %d",
			b.OperatorID, b.Layer, expectedLeader)
	}
	if len(b.Value) == 0 {
		return errors.New("twoab: phase-1 bundle has empty Value")
	}
	if b.Layer == 0 && len(b.L0Witness) == 0 {
		return errors.New("twoab: phase-1 bundle at L_0 has empty L0Witness")
	}
	return nil
}

// ValidateValueMsg checks structural invariants for an incoming Phase-2a
// ValueMsg envelope. Per spec §Phase 2a, an op emits exactly one ValueMsg
// per (slot) — either at the Phase-2a fire-instant (initial emission) or
// as the A1 upgrade from a prior NoValueMsg. A second distinct ValueMsg
// from the same op is Rule-6a (Phase-2 equivocation) slashable; caught at
// Instance.ObserveValue, not here — this function only does per-message
// structural checks.
func ValidateValueMsg(v *ValueMsg, cfg *Config) error {
	if v == nil {
		return errors.New("twoab: nil ValueMsg")
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if v.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: ValueMsg cluster id %x != instance cluster id %x",
			v.ClusterID, cfg.ClusterID)
	}
	if v.Height != cfg.Height {
		return fmt.Errorf("twoab: ValueMsg height %d != instance height %d", v.Height, cfg.Height)
	}
	if !obft.OperatorInCluster(v.OperatorID, cfg.Operators) {
		return fmt.Errorf("twoab: ValueMsg sender %d not in cluster", v.OperatorID)
	}
	if len(v.V) == 0 {
		return errors.New("twoab: ValueMsg has empty V at L_0")
	}
	if v.ValueRoot != ValueRoot(v.V) {
		return fmt.Errorf("twoab: ValueMsg ValueRoot %x does not match sha256(V)",
			v.ValueRoot)
	}
	// Op11: L0Witness is required non-empty. Every honest emitter has a
	// retained Phase-1 bundle with the leader's witness (either directly
	// observed or harvested via the peer-reflood path). The wire-level
	// L0Witness check is structural; BLS verification + harvest happens at
	// the Instance layer (ObserveValueMsg) against the L_0 leader's
	// pubKeyShare. A peer presenting an empty L0Witness can't claim
	// σ-direction — they should have emitted KindNoValue instead.
	if len(v.L0Witness) == 0 {
		return errors.New("twoab: ValueMsg has empty L0Witness (Op11 requires the L_0 leader's forwarded σ partial)")
	}
	// Op5: L0Partial (the emitter's own σ partial on V at L_0) is required
	// non-empty. KindValue under V3 is the terminal σ-side emission — an
	// op cannot claim σ-direction at L_0 without presenting its own σ
	// partial on V. BLS verify happens at the Instance layer
	// (ObserveValueMsg) against pubKeyShares[v.OperatorID]; a fake
	// L0Partial fires Rule 5 against the emitter.
	if len(v.L0Partial) == 0 {
		return errors.New("twoab: ValueMsg has empty L0Partial (Op5 requires the emitter's own σ partial on V at L_0)")
	}
	if err := validateLayerEntries(v.LayerEntries, cfg, "ValueMsg"); err != nil {
		return err
	}
	return nil
}

// ValidateNoValueMsg checks structural invariants for an incoming Phase-2a
// NoValueMsg envelope. The NoValueMsg has no L_0 payload; only K-1
// LayerEntries for deeper layers.
func ValidateNoValueMsg(nv *NoValueMsg, cfg *Config) error {
	if nv == nil {
		return errors.New("twoab: nil NoValueMsg")
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if nv.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: NoValueMsg cluster id %x != instance cluster id %x",
			nv.ClusterID, cfg.ClusterID)
	}
	if nv.Height != cfg.Height {
		return fmt.Errorf("twoab: NoValueMsg height %d != instance height %d", nv.Height, cfg.Height)
	}
	if !obft.OperatorInCluster(nv.OperatorID, cfg.Operators) {
		return fmt.Errorf("twoab: NoValueMsg sender %d not in cluster", nv.OperatorID)
	}
	if err := validateLayerEntries(nv.LayerEntries, cfg, "NoValueMsg"); err != nil {
		return err
	}
	return nil
}

// ValidateCommit checks structural invariants for an incoming Phase-2b
// (or Phase-2a NRDirect) Commit envelope. Post Op5, Commit is NR-side
// only — the σ-side terminal emission moved into KindValue.
//
// Per spec §Wire format:
//   - Side=NR: L0Value empty + L0Partial non-empty + LayerEntries empty.
//   - Side=NRDirect: L0Value empty + L0Partial non-empty + LayerEntries
//     present (K-1 entries; same structural rules as Phase-2a emissions).
//
// The historical Side=Signed value (0x01) is now invalid and rejected
// here — surfaces wire-version drift between cluster members loudly
// (catches a peer on the pre-Op5 wire format trying to talk to an Op5
// receiver).
func ValidateCommit(c *Commit, cfg *Config) error {
	if c == nil {
		return errors.New("twoab: nil Commit")
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if c.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: Commit cluster id %x != instance cluster id %x",
			c.ClusterID, cfg.ClusterID)
	}
	if c.Height != cfg.Height {
		return fmt.Errorf("twoab: Commit height %d != instance height %d", c.Height, cfg.Height)
	}
	if !obft.OperatorInCluster(c.OperatorID, cfg.Operators) {
		return fmt.Errorf("twoab: Commit sender %d not in cluster", c.OperatorID)
	}
	if len(c.L0Partial) == 0 {
		return errors.New("twoab: Commit has empty L0Partial")
	}
	// Post Op5: L0Value is unused (was for Side=Signed which no longer
	// exists). Reject non-empty L0Value on any Commit side to catch
	// stragglers from the pre-Op5 wire format.
	if len(c.L0Value) != 0 {
		return errors.New("twoab: Commit has non-empty L0Value (post Op5 the σ-side moved into KindValue.L0Partial; Commit is NR-side only)")
	}
	switch c.Side {
	case CommitSideNR:
		if len(c.LayerEntries) != 0 {
			return errors.New("twoab: Commit Side=NR must have empty LayerEntries (L_k>0 entries live in earlier KindNoValue emission)")
		}
	case CommitSideNRDirect:
		if err := validateLayerEntries(c.LayerEntries, cfg, "Commit/NRDirect"); err != nil {
			return err
		}
	case CommitSideUnspecified:
		return errors.New("twoab: Commit Side is unspecified")
	default:
		// 0x01 was the pre-Op5 CommitSideSigned value. Catch it here as
		// an invalid side to make wire-version drift visible.
		return fmt.Errorf("twoab: Commit Side 0x%02x is invalid (post Op5 only NR=0x02 and NRDirect=0x03 are valid; 0x01 was the removed Signed side)", byte(c.Side))
	}
	return nil
}

// ValidateCertificate checks structural invariants for an incoming
// Certificate (KindCertificate payload).
func ValidateCertificate(c *Certificate, cfg *Config) error {
	if c == nil {
		return errors.New("twoab: nil certificate")
	}
	if cfg == nil {
		return ErrNilConfig
	}
	if c.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: certificate cluster id %x != instance cluster id %x",
			c.ClusterID, cfg.ClusterID)
	}
	if c.Height != cfg.Height {
		return fmt.Errorf("twoab: certificate height %d != instance height %d",
			c.Height, cfg.Height)
	}
	if len(c.Value) == 0 {
		return errors.New("twoab: certificate has empty Value")
	}
	if len(c.Signature) == 0 {
		return errors.New("twoab: certificate has empty Signature")
	}
	return nil
}

// validateLayerEntries checks the K-1 LayerEntries array carried inside a
// Phase-2a emission (ValueMsg / NoValueMsg / Commit-NRDirect). Per spec
// §Wire format, entries must:
//
//   - Be exactly K-1 in count (one entry per layer k ∈ [1, K-1]).
//   - Have Layer fields covering exactly {1, 2, ..., K-1}, in order, no duplicates.
//   - Have a Kind in {Empty, SigmaChained, NRPlaintext}.
//   - For SigmaChained: V non-empty + Payload non-empty.
//   - For NRPlaintext: V empty + Payload non-empty + Layer ∈ [1, K-2]
//     (no nr_tag at K-1 deepest).
//   - For Empty: V empty + Payload empty.
//
// `kindLabel` is used in error messages to identify the carrying envelope
// (e.g. "ValueMsg", "Commit/NRDirect").
func validateLayerEntries(entries []LayerEntry, cfg *Config, kindLabel string) error {
	K := cfg.K()
	want := K - 1
	if len(entries) != want {
		return fmt.Errorf("twoab: %s LayerEntries count %d != K-1 = %d",
			kindLabel, len(entries), want)
	}
	for idx, e := range entries {
		expectedLayer := idx + 1
		if e.Layer != expectedLayer {
			return fmt.Errorf("twoab: %s LayerEntries[%d] has Layer %d, expected %d",
				kindLabel, idx, e.Layer, expectedLayer)
		}
		// (The prior `e.Layer < 1 || e.Layer >= K` range check was
		// removed: the `e.Layer != expectedLayer` mismatch above already
		// constrains e.Layer to idx+1 for idx ∈ [0, K-2], so
		// e.Layer ∈ [1, K-1] is structurally guaranteed by the time we
		// reach this point.)
		switch e.Kind {
		case LayerEntryEmpty:
			if len(e.V) != 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=Empty has non-empty V",
					kindLabel, idx)
			}
			if len(e.Payload) != 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=Empty has non-empty Payload",
					kindLabel, idx)
			}
		case LayerEntrySigmaChained:
			if len(e.V) == 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=SigmaChained has empty V",
					kindLabel, idx)
			}
			if len(e.Payload) == 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=SigmaChained has empty Payload",
					kindLabel, idx)
			}
		case LayerEntryNRPlaintext:
			if e.Layer >= K-1 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=NRPlaintext at Layer %d invalid (no nr_tag at deepest layer K-1=%d)",
					kindLabel, idx, e.Layer, K-1)
			}
			if len(e.V) != 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=NRPlaintext has non-empty V",
					kindLabel, idx)
			}
			if len(e.Payload) == 0 {
				return fmt.Errorf("twoab: %s LayerEntries[%d] Kind=NRPlaintext has empty Payload",
					kindLabel, idx)
			}
		default:
			return fmt.Errorf("twoab: %s LayerEntries[%d] Kind 0x%02x is invalid",
				kindLabel, idx, byte(e.Kind))
		}
	}
	return nil
}

