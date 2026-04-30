package tbft

import (
	"bytes"
	"errors"
	"fmt"
)

// Validation functions for TBFT messages received from peers.
//
// These are *structural* checks: they verify the protocol-level shape of
// incoming Onion / NonReceiptAttestation messages against the Config of
// the instance the message belongs to. They do NOT:
//
//   - Verify the sender's identity (authentication is the SSV adapter's
//     concern; by the time the TBFT core sees a message, the adapter has
//     already verified the libp2p / operator-key signature).
//   - Verify the cryptographic contents of the message (per-partial-sig
//     verification happens at Resolve time, when partials are aggregated).
//   - Check rate limits or replay (those are SSV-adapter concerns; the
//     Instance silently ignores duplicate-from-same-operator observations).
//
// Standalone functions (rather than only Observe* methods) so the SSV
// adapter can validate before queuing or before deciding to accept a peer
// message at all.

// ValidateOnion checks that an Onion is well-formed for the given Config.
//
// Rules:
//   - Non-nil
//   - Height matches cfg.Height
//   - len(Layers) == cfg.K()
//   - OperatorID is a member of cfg.Operators
//   - Each layer is either fully empty (no contribution) or fully populated
//     (both Value and Ciphertext non-empty)
//   - Each non-empty layer's Tag matches cfg.LayerTag(k) — i.e. layer 0
//     has Tag == nil, layers 1..K-1 have Tag == NoQuorumTag(k-1)
func ValidateOnion(o *Onion, cfg *Config) error {
	if o == nil {
		return errors.New("tbft: nil onion")
	}
	if cfg == nil {
		return errors.New("tbft: nil config")
	}
	if o.Height != cfg.Height {
		return fmt.Errorf("tbft: onion height %d != instance height %d", o.Height, cfg.Height)
	}
	if len(o.Layers) != cfg.K() {
		return fmt.Errorf("tbft: onion has %d layers, expected K=%d", len(o.Layers), cfg.K())
	}
	if !operatorInCluster(o.OperatorID, cfg) {
		return fmt.Errorf("tbft: onion sender %d not in cluster", o.OperatorID)
	}

	for k, el := range o.Layers {
		hasValue := len(el.Value) > 0
		hasCipher := len(el.Ciphertext) > 0

		// Layer is either fully empty (no contribution) or fully populated.
		if hasValue != hasCipher {
			return fmt.Errorf("tbft: layer %d half-populated (Value=%v, Ciphertext=%v)",
				k, hasValue, hasCipher)
		}
		if !hasValue {
			continue // empty layer — operator did not contribute at this index
		}

		// Tag must match the canonical LayerTag(k) for this Config.
		expectedTag := cfg.LayerTag(k)
		if !bytes.Equal(el.Tag, expectedTag) {
			return fmt.Errorf("tbft: layer %d tag mismatch (got %d bytes, expected %d bytes)",
				k, len(el.Tag), len(expectedTag))
		}
	}
	return nil
}

// ValidateNonReceipt checks that a NonReceiptAttestation is well-formed for
// the given Config.
//
// Rules:
//   - Non-nil
//   - Height matches cfg.Height
//   - Layer is in [0, K-1) — non-receipt for the last layer has no purpose
//   - OperatorID is a member of cfg.Operators
//   - PartialSig is non-empty (no validation of its cryptographic content;
//     that happens at AggregatePartials time)
func ValidateNonReceipt(nr *NonReceiptAttestation, cfg *Config) error {
	if nr == nil {
		return errors.New("tbft: nil non-receipt")
	}
	if cfg == nil {
		return errors.New("tbft: nil config")
	}
	if nr.Height != cfg.Height {
		return fmt.Errorf("tbft: non-receipt height %d != instance height %d",
			nr.Height, cfg.Height)
	}
	if nr.Layer < 0 || nr.Layer >= cfg.K()-1 {
		return fmt.Errorf("tbft: non-receipt layer %d out of valid range [0, %d)",
			nr.Layer, cfg.K()-1)
	}
	if !operatorInCluster(nr.OperatorID, cfg) {
		return fmt.Errorf("tbft: non-receipt sender %d not in cluster", nr.OperatorID)
	}
	if len(nr.PartialSig) == 0 {
		return errors.New("tbft: non-receipt has empty PartialSig")
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
