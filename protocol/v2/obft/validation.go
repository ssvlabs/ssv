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

// ValidateOnion checks structural invariants for an incoming Onion (KindOnion
// payload).
//
// Per spec §Phase 2, Onions may be emitted multiple times per (slot, operator)
// as σ-eligibility transitions late; this function validates a single Onion
// instance, leaving cumulative-tracking semantics to the caller / Instance.
func ValidateOnion(o *Onion, cfg *Config) error {
	if o == nil {
		return errors.New("obft: nil onion")
	}
	if cfg == nil {
		return errors.New("obft: nil config")
	}
	if o.Height != cfg.Height {
		return fmt.Errorf("obft: onion height %d != instance height %d", o.Height, cfg.Height)
	}
	if len(o.Layers) != cfg.K() {
		return fmt.Errorf("obft: onion has %d layers, expected K=%d", len(o.Layers), cfg.K())
	}
	if !operatorInCluster(o.OperatorID, cfg) {
		return fmt.Errorf("obft: onion sender %d not in cluster", o.OperatorID)
	}

	for k, el := range o.Layers {
		hasValue := len(el.Value) > 0
		hasCipher := len(el.Ciphertext) > 0
		// Each layer is either fully empty (no contribution at this layer)
		// or fully populated. Half-populated entries are malformed.
		if hasValue != hasCipher {
			return fmt.Errorf("obft: onion layer %d half-populated (Value=%v, Ciphertext=%v)",
				k, hasValue, hasCipher)
		}
	}
	return nil
}

// ValidateNR checks structural invariants for an incoming NR (KindNR
// payload).
//
// Per spec §Phase 2, KindNR is emitted at most once per (slot, operator) at
// end-of-Phase-2 force-commit, carrying NR partials for the layers the
// operator committed NR-side at.
func ValidateNR(nr *NR, cfg *Config) error {
	if nr == nil {
		return errors.New("obft: nil NR message")
	}
	if cfg == nil {
		return errors.New("obft: nil config")
	}
	if nr.Height != cfg.Height {
		return fmt.Errorf("obft: NR message height %d != instance height %d",
			nr.Height, cfg.Height)
	}
	if !operatorInCluster(nr.OperatorID, cfg) {
		return fmt.Errorf("obft: NR sender %d not in cluster", nr.OperatorID)
	}
	seenLayers := make(map[int]bool, len(nr.Partials))
	for _, p := range nr.Partials {
		// NR exists only for layers that have a successor (k in [0, K-1)).
		// The deepest layer (K-1) has no NR tag — there is no further
		// layer to advance to.
		if p.Layer < 0 || p.Layer >= cfg.K()-1 {
			return fmt.Errorf("obft: NR partial layer %d out of valid range [0, %d)",
				p.Layer, cfg.K()-1)
		}
		if seenLayers[p.Layer] {
			return fmt.Errorf("obft: NR has duplicate layer %d", p.Layer)
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
