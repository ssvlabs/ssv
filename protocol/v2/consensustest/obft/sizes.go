package obft

import (
	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Wire-size accounting for OBFT messages. Sizes use standardized BLS field
// widths (ct.StubSignatureSize etc.) so bandwidth measurements in stub mode
// match real-BLS mode at the byte level — only signing/verification CPU cost
// differs across modes, not wire size.
//
// Per OBFT.md §Phase 1 / §Phase 2 / §Final-certificate gossip wire formats:
//
//	Phase1Bundle:  ClusterID(32) + OperatorID(8) + Height(8) + Layer(4) +
//	               Value(|V|) + LeaderSigma(BLS sig)
//	Commit:        ClusterID(32) + OperatorID(8) + Height(8) +
//	               K layers × (Value(|V|) + Ciphertext(BLS sig + IBE overhead at L_k>0)) +
//	               NRPartials × (Layer(4) + PartialSig(BLS sig)) +
//	               Witnesses × (Layer(4) + Leader(8) + ValueRoot(32) + Sigma(BLS sig))
//	Certificate:   ClusterID(32) + Height(8) + Value(|V|) + Signature(BLS sig)
//
// Excludes outer SignedSSVMessage envelope overhead (wrapper not modeled
// here — the OBFT adapter operates one layer below the outer envelope, so
// the bandwidth counts inner-message bytes only).
const (
	clusterIDBytes  = 32
	operatorIDBytes = 8
	heightBytes     = 8
	layerBytes      = 4

	// witnessFramingOverhead accounts for length-prefix / framing bytes per
	// LeaderSigmaWitness on the wire — per OBFT.md §Phase 2 / wire format
	// "per witness ≈ 4 + 8 + 32 + 96 + length-prefix overhead ≈ 145 bytes".
	// Layer + Leader + ValueRoot + Sigma = 140 B raw; framing brings it to
	// ~145 B. Keep this as a named constant so bandwidth bands match the
	// spec's quoted cluster-wide witness cost (~2.3 KB at K=4, n=4).
	witnessFramingOverhead = 5
)

func phase1BundleSize(b *obftbase.Phase1Bundle) int64 {
	return clusterIDBytes + operatorIDBytes + heightBytes + layerBytes +
		int64(len(b.Value)) + ct.StubSignatureSize
}

func commitSize(c *obftbase.Commit) int64 {
	size := int64(clusterIDBytes + operatorIDBytes + heightBytes)
	for layer, el := range c.Layers {
		if len(el.Value) == 0 {
			continue
		}
		size += int64(len(el.Value))
		if layer == 0 {
			size += ct.StubSignatureSize
		} else {
			size += ct.StubSignatureSize + ct.StubIBECiphertextOverhead
		}
	}
	for range c.NRPartials {
		size += layerBytes + ct.StubSignatureSize
	}
	for range c.Witnesses {
		// Layer + Leader + ValueRoot (32 fixed) + Sigma + framing overhead
		// = 4 + 8 + 32 + 96 + 5 ≈ 145 B per OBFT.md §Phase 2 wire-format quote.
		size += layerBytes + operatorIDBytes + 32 + ct.StubSignatureSize + witnessFramingOverhead
	}
	return size
}

func certSize(c *obftbase.Certificate) int64 {
	return clusterIDBytes + heightBytes + int64(len(c.Value)) + ct.StubSignatureSize
}
