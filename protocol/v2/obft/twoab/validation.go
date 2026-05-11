package twoab

import (
	"errors"
	"fmt"
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
// Phase-1 bundle. Per spec §Phase 1 (Variant C), the bundle carries no
// σ_V threshold partial — only the value + envelope-layer auth.
func ValidatePhase1Bundle(b *Phase1Bundle, cfg *Config) error {
	if b == nil {
		return errors.New("twoab: nil phase-1 bundle")
	}
	if cfg == nil {
		return errors.New("twoab: nil config")
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
	return nil
}

// ValidateVerdict checks structural invariants for an incoming Phase-2a
// Verdict envelope. Per spec §Phase 2a, every operator emits exactly one
// Verdict per (slot, layer); a second distinct verdict is Rule-6a slashable
// (caught at Instance.ObserveVerdict, not here — this function only does
// per-message structural checks).
func ValidateVerdict(v *Verdict, cfg *Config) error {
	if v == nil {
		return errors.New("twoab: nil verdict")
	}
	if cfg == nil {
		return errors.New("twoab: nil config")
	}
	if v.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: verdict cluster id %x != instance cluster id %x",
			v.ClusterID, cfg.ClusterID)
	}
	if v.Height != cfg.Height {
		return fmt.Errorf("twoab: verdict height %d != instance height %d", v.Height, cfg.Height)
	}
	if v.Layer < 0 || v.Layer >= cfg.K() {
		return fmt.Errorf("twoab: verdict layer %d out of range [0, %d)", v.Layer, cfg.K())
	}
	if !operatorInCluster(v.OperatorID, cfg) {
		return fmt.Errorf("twoab: verdict sender %d not in cluster", v.OperatorID)
	}
	switch v.Kind {
	case VerdictSigmaV:
		if v.ValueRoot == ([32]byte{}) {
			return errors.New("twoab: σV verdict has zero ValueRoot")
		}
	case VerdictNR, VerdictNV:
		// Per spec §Phase 2a, value_root = null (zero bytes) for NR and NV.
		if v.ValueRoot != ([32]byte{}) {
			return fmt.Errorf("twoab: %s verdict must have zero ValueRoot, got %x",
				v.Kind, v.ValueRoot)
		}
	case VerdictUnspecified:
		return errors.New("twoab: verdict kind is unspecified")
	default:
		return fmt.Errorf("twoab: verdict kind 0x%02x is invalid", byte(v.Kind))
	}
	return nil
}

// ValidateOnion2b checks structural invariants for an incoming Phase-2b
// Onion2b message.
//
// Per spec §Phase 2b emission, Onion2b is emitted exactly once per (slot,
// operator) at T_commit, bundling the operator's σ-side onion (one entry
// per layer) and NR-side partials (one entry per NR-state layer in
// [0, K-1)).
//
// Per-layer commitment is exclusive within an Onion2b: a layer is either
// σ-side (entry in Layers[k] populated) OR NR-side (entry in NRPartials),
// never both. Cross-checking σ + NR for the same layer is part of Rule 1
// detection, enforced by Instance.ObserveOnion2b (not here).
func ValidateOnion2b(o *Onion2b, cfg *Config) error {
	if o == nil {
		return errors.New("twoab: nil onion2b")
	}
	if cfg == nil {
		return errors.New("twoab: nil config")
	}
	if o.ClusterID != cfg.ClusterID {
		return fmt.Errorf("twoab: onion2b cluster id %x != instance cluster id %x",
			o.ClusterID, cfg.ClusterID)
	}
	if o.Height != cfg.Height {
		return fmt.Errorf("twoab: onion2b height %d != instance height %d", o.Height, cfg.Height)
	}
	if len(o.Layers) != cfg.K() {
		return fmt.Errorf("twoab: onion2b has %d σ layers, expected K=%d", len(o.Layers), cfg.K())
	}
	if !operatorInCluster(o.OperatorID, cfg) {
		return fmt.Errorf("twoab: onion2b sender %d not in cluster", o.OperatorID)
	}

	for k, el := range o.Layers {
		hasValue := len(el.Value) > 0
		hasCipher := len(el.Ciphertext) > 0
		// Each σ layer is either fully empty (no σ contribution at this
		// layer) or fully populated. Half-populated entries are malformed.
		if hasValue != hasCipher {
			return fmt.Errorf("twoab: onion2b σ layer %d half-populated (Value=%v, Ciphertext=%v)",
				k, hasValue, hasCipher)
		}
	}

	seenLayers := make(map[int]bool, len(o.NRPartials))
	for _, p := range o.NRPartials {
		// NR exists only for layers that have a successor (k in [0, K-1)).
		// The deepest layer (K-1) has no NR tag — there is no further layer
		// to advance to, so NR there produces no on-wire emission per spec
		// §Convergence rule "Deepest-layer NR has no on-wire emission".
		if p.Layer < 0 || p.Layer >= cfg.K()-1 {
			return fmt.Errorf("twoab: NR partial layer %d out of valid range [0, %d)",
				p.Layer, cfg.K()-1)
		}
		if seenLayers[p.Layer] {
			return fmt.Errorf("twoab: onion2b has duplicate NR layer %d", p.Layer)
		}
		seenLayers[p.Layer] = true
		if len(p.PartialSig) == 0 {
			return fmt.Errorf("twoab: NR partial at layer %d has empty signature", p.Layer)
		}
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
		return errors.New("twoab: nil config")
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

func operatorInCluster(id OperatorID, cfg *Config) bool {
	for _, op := range cfg.Operators {
		if op == id {
			return true
		}
	}
	return false
}
