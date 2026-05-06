package obft

import (
	"errors"
	"fmt"
)

// Structural validation for OBFT messages received from peers.
//
// These are protocol-level shape checks: the message is well-formed for the
// instance's Config (right Height, right K, etc.). They do NOT:
//
//   - Verify the sender's identity (the SSV adapter has already verified the
//     outer SignedSSVMessage signature by the time the inner message reaches
//     the obft core).
//   - Verify the inner partial-sig cryptography (per-partial verification
//     happens at observation time inside Instance methods, against the
//     pubKeyShares the Instance was constructed with).
//
// Standalone functions (rather than only Instance methods) so the SSV adapter
// can validate before queuing or before deciding to accept the message at
// all.

// ValidatePhase1Bundle checks structural invariants for an incoming
// Phase-1 bundle.
func ValidatePhase1Bundle(b *Phase1Bundle, cfg *Config) error {
	if b == nil {
		return errors.New("obft: nil phase-1 bundle")
	}
	if cfg == nil {
		return errors.New("obft: nil config")
	}
	if b.Height != cfg.Height {
		return fmt.Errorf("obft: phase-1 bundle height %d != instance height %d", b.Height, cfg.Height)
	}
	if b.Layer < 0 || b.Layer >= cfg.K() {
		return fmt.Errorf("obft: phase-1 bundle layer %d out of range [0, %d)", b.Layer, cfg.K())
	}
	expectedLeader := cfg.Layers[b.Layer].Leader
	if b.OperatorID != expectedLeader {
		return fmt.Errorf("obft: phase-1 bundle from operator %d but layer %d's leader is %d",
			b.OperatorID, b.Layer, expectedLeader)
	}
	if len(b.Value) == 0 {
		return errors.New("obft: phase-1 bundle has empty Value")
	}
	if len(b.SigmaV) == 0 {
		return errors.New("obft: phase-1 bundle has empty SigmaV")
	}
	return nil
}

// ValidateCommit checks structural invariants for an incoming Commit
// (KindCommit payload).
//
// Per spec §Phase 2, KindCommit is emitted exactly once per (slot, operator)
// at T_commit, bundling the operator's σ-side onion (one entry per layer)
// and NR-side partials (one entry per NR-state layer in [0, K-1)).
//
// Per-layer commitment is exclusive within a Commit: a layer is either σ-side
// (entry in Layers[k] populated) OR NR-side (entry in NRPartials), never both.
// Cross-checking σ + NR for the same layer is part of cross-signing detection
// (Rule 1), enforced by Instance.ObserveCommit.
func ValidateCommit(c *Commit, cfg *Config) error {
	if c == nil {
		return errors.New("obft: nil commit")
	}
	if cfg == nil {
		return errors.New("obft: nil config")
	}
	if c.Height != cfg.Height {
		return fmt.Errorf("obft: commit height %d != instance height %d", c.Height, cfg.Height)
	}
	if len(c.Layers) != cfg.K() {
		return fmt.Errorf("obft: commit has %d σ layers, expected K=%d", len(c.Layers), cfg.K())
	}
	if !operatorInCluster(c.OperatorID, cfg) {
		return fmt.Errorf("obft: commit sender %d not in cluster", c.OperatorID)
	}

	for k, el := range c.Layers {
		hasValue := len(el.Value) > 0
		hasCipher := len(el.Ciphertext) > 0
		// Each σ layer is either fully empty (no σ contribution) or fully
		// populated. Half-populated entries are malformed.
		if hasValue != hasCipher {
			return fmt.Errorf("obft: commit σ layer %d half-populated (Value=%v, Ciphertext=%v)",
				k, hasValue, hasCipher)
		}
	}

	seenLayers := make(map[int]bool, len(c.NRPartials))
	for _, p := range c.NRPartials {
		// NR exists only for layers that have a successor (k in [0, K-1)).
		// The deepest layer (K-1) has no NR tag — there is no further
		// layer to advance to.
		if p.Layer < 0 || p.Layer >= cfg.K()-1 {
			return fmt.Errorf("obft: NR partial layer %d out of valid range [0, %d)",
				p.Layer, cfg.K()-1)
		}
		if seenLayers[p.Layer] {
			return fmt.Errorf("obft: commit has duplicate NR layer %d", p.Layer)
		}
		seenLayers[p.Layer] = true
		if len(p.PartialSig) == 0 {
			return fmt.Errorf("obft: NR partial at layer %d has empty signature", p.Layer)
		}
	}
	return nil
}

// ValidateCertificate checks structural invariants for an incoming
// Certificate (KindCertificate payload).
func ValidateCertificate(c *Certificate, cfg *Config) error {
	if c == nil {
		return errors.New("obft: nil certificate")
	}
	if cfg == nil {
		return errors.New("obft: nil config")
	}
	if c.Height != cfg.Height {
		return fmt.Errorf("obft: certificate height %d != instance height %d",
			c.Height, cfg.Height)
	}
	if len(c.Value) == 0 {
		return errors.New("obft: certificate has empty Value")
	}
	if len(c.Signature) == 0 {
		return errors.New("obft: certificate has empty Signature")
	}
	return nil
}

func operatorInCluster(id OperatorID, cfg *Config) bool {
	for _, op := range cfg.Operators {
		if op == id {
			return true
		}
	}
	return false
}
