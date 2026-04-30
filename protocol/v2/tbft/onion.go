package tbft

import (
	"errors"
	"fmt"
)

// BuildOnion constructs an operator's K-layer onion given:
//
//   - cfg          — the consensus config (gives K, ClusterID, Height, tags)
//   - share        — operator's BLS secret share (used to sign each layer's value)
//   - clusterPubKey — the validator's BLS pubkey (the IBE trust anchor)
//   - candidates    — the operator's local view of the candidate value at each
//     layer. Layers absent from this map have no contribution from this
//     operator (the onion's layer at that index will be zero, and the
//     operator should emit a NonReceiptAttestation for that layer
//     separately if they want layer skip to be possible).
//   - signer        — Signer used to produce partial signatures
//   - ibe           — ThresholdIBE used to encrypt non-zero layers
//
// Returns the onion ready for broadcast.
//
// Layer 0 is plaintext; layers 1..K-1 are encrypted under their respective
// LayerTag(k) (= NoQuorumTag(k-1)).
func BuildOnion(
	cfg *Config,
	operatorID OperatorID,
	share []byte,
	clusterPubKey []byte,
	candidates map[int]Value,
	signer Signer,
	ibe ThresholdIBE,
) (*Onion, error) {
	if cfg == nil {
		return nil, errors.New("tbft: nil config")
	}
	if signer == nil || ibe == nil {
		return nil, errors.New("tbft: nil signer or ibe")
	}

	K := cfg.K()
	layers := make([]EncryptedLayer, K)

	for k := 0; k < K; k++ {
		v, ok := candidates[k]
		if !ok {
			// No candidate at layer k — this operator contributes nothing
			// at this layer. The protocol treats a zero EncryptedLayer
			// (nil Tag, nil Ciphertext) as "no contribution."
			continue
		}

		partial, err := signer.SignPartial(share, v)
		if err != nil {
			return nil, fmt.Errorf("tbft: signing layer %d: %w", k, err)
		}

		tag := cfg.LayerTag(k)
		if tag == nil {
			// Layer 0 — plaintext.
			layers[k] = EncryptedLayer{
				Tag:        nil,
				Value:      append(Value{}, v...),
				Ciphertext: partial,
			}
			continue
		}

		ct, err := ibe.Encrypt(clusterPubKey, tag, partial)
		if err != nil {
			return nil, fmt.Errorf("tbft: encrypting layer %d: %w", k, err)
		}
		layers[k] = EncryptedLayer{
			Tag:        tag,
			Value:      append(Value{}, v...),
			Ciphertext: ct,
		}
	}

	return &Onion{
		OperatorID: operatorID,
		Height:     cfg.Height,
		Layers:     layers,
	}, nil
}

// BuildNonReceipt constructs a NonReceiptAttestation for layer `layer`,
// stating that this operator did not have a valid candidate at that layer.
// The attestation is a partial BLS signature on NoQuorumTag(layer); 2f+1 of
// these aggregate to the IBE decryption key for layer `layer + 1`.
//
// `layer` must be in [0, K-1). The last layer (K-1) cannot be skipped
// further, so a non-receipt for it has no purpose.
func BuildNonReceipt(
	cfg *Config,
	operatorID OperatorID,
	share []byte,
	layer int,
	signer Signer,
) (*NonReceiptAttestation, error) {
	if cfg == nil {
		return nil, errors.New("tbft: nil config")
	}
	if signer == nil {
		return nil, errors.New("tbft: nil signer")
	}
	if layer < 0 || layer >= cfg.K()-1 {
		return nil, fmt.Errorf("tbft: non-receipt layer %d out of range [0, %d)", layer, cfg.K()-1)
	}

	tag := NoQuorumTag(cfg.ClusterID, cfg.Height, layer)
	partial, err := signer.SignPartial(share, tag)
	if err != nil {
		return nil, fmt.Errorf("tbft: signing non-receipt for layer %d: %w", layer, err)
	}

	return &NonReceiptAttestation{
		OperatorID: operatorID,
		Height:     cfg.Height,
		Layer:      layer,
		PartialSig: partial,
	}, nil
}
