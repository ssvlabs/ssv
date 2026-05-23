package obft

import "fmt"

// ChainEncryptForLayer wraps `partial` in the layer-k chained-IBE onion:
//
//	E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( partial ) ... )
//
// with nr_tag_0 outermost. At k == 0 it returns `partial` unchanged (L_0 is
// plaintext). Shared by both OBFT protocols; each Instance's
// chainEncryptForLayer delegates here.
func ChainEncryptForLayer(ibe ThresholdIBE, clusterPubKey []byte, clusterID [32]byte, height Height, k int, partial []byte) ([]byte, error) {
	if k == 0 {
		return partial, nil
	}
	inner := partial
	// Wrap from innermost (nr_tag_{k-1}) to outermost (nr_tag_0).
	for j := k - 1; j >= 0; j-- {
		tag := NoQuorumTag(clusterID, height, j)
		ct, err := ibe.Encrypt(clusterPubKey, tag, inner)
		if err != nil {
			return nil, fmt.Errorf("obft: chained-IBE encrypt at level %d: %w", j, err)
		}
		inner = ct
	}
	return inner, nil
}

// ChainDecryptForLayer peels the layer-k chained-IBE onion using
// decryptionKeys[0..k-1] (the aggregated NR-partials sigs on
// nr_tag_0..nr_tag_{k-1}), applied outermost-first. At k == 0 it returns
// `ciphertext` unchanged. Shared by both OBFT protocols.
func ChainDecryptForLayer(ibe ThresholdIBE, k int, ciphertext []byte, decryptionKeys [][]byte) ([]byte, error) {
	if k == 0 {
		return ciphertext, nil
	}
	if len(decryptionKeys) < k {
		return nil, fmt.Errorf("obft: need %d chained-decryption keys for layer %d, have %d", k, k, len(decryptionKeys))
	}
	outer := ciphertext
	for j := 0; j < k; j++ {
		pt, err := ibe.Decrypt(outer, decryptionKeys[j])
		if err != nil {
			return nil, fmt.Errorf("obft: chained-IBE decrypt at level %d: %w", j, err)
		}
		outer = pt
	}
	return outer, nil
}
