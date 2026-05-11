package twoab

import (
	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Wire-size accounting for 2abOBFT messages. Sizes use standardized BLS
// field widths (ct.StubSignatureSize etc.) so bandwidth measurements in
// stub mode match real-BLS mode at the byte level — only signing/
// verification CPU cost differs across modes, not wire size.
//
// Per docs/2abOBFT.md §Phase 1 / §Phase 2a / §Phase 2b / §Final-certificate
// gossip wire formats:
//
//	Phase1Bundle:  ClusterID(32) + OperatorID(8) + Height(8) + Layer(4) +
//	               Value(|V|)   ← NO σ_V partial under Variant C
//	Verdict:       ClusterID(32) + OperatorID(8) + Height(8) + Layer(4) +
//	               Kind(1) + ValueRoot(32)
//	Onion2b:       ClusterID(32) + OperatorID(8) + Height(8) +
//	               K layers × (Value(|V|) + Ciphertext(BLS sig + IBE overhead at L_k>0)) +
//	               NRPartials × (Layer(4) + PartialSig(BLS sig))
//	               ← NO Witnesses array (no Phase-1 σ_V exists)
//	Certificate:   ClusterID(32) + Height(8) + Value(|V|) + Signature(BLS sig)
//
// Excludes outer SignedSSVMessage envelope overhead (wrapper not modeled
// here — the 2ab adapter operates one layer below the outer envelope, so
// the bandwidth counts inner-message bytes only).
const (
	clusterIDBytes  = 32
	operatorIDBytes = 8
	heightBytes     = 8
	layerBytes      = 4

	// verdictKindBytes is the wire size of VerdictKind on the encoded
	// Phase-2a Verdict envelope (one byte).
	verdictKindBytes = 1
	// valueRootBytes is the wire size of ValueRoot ([32]byte hash).
	valueRootBytes = 32
)

func phase1BundleSize(b *twoab.Phase1Bundle) int64 {
	return clusterIDBytes + operatorIDBytes + heightBytes + layerBytes +
		int64(len(b.Value))
}

func verdictSize(_ *twoab.Verdict) int64 {
	// Fixed-size envelope: cluster ID + op ID + height + layer + kind + value_root.
	return clusterIDBytes + operatorIDBytes + heightBytes + layerBytes +
		verdictKindBytes + valueRootBytes
}

func onion2bSize(o *twoab.Onion2b) int64 {
	size := int64(clusterIDBytes + operatorIDBytes + heightBytes)
	for layer, el := range o.Layers {
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
	for range o.NRPartials {
		size += layerBytes + ct.StubSignatureSize
	}
	return size
}

func certSize(c *twoab.Certificate) int64 {
	return clusterIDBytes + heightBytes + int64(len(c.Value)) + ct.StubSignatureSize
}
