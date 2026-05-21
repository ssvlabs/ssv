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
//	               Value(|V|)
//	ValueMsg:      ClusterID(32) + OperatorID(8) + Height(8) +
//	               Value(|V|) + ValueRoot(32) +
//	               K-1 LayerEntries × (Layer(4) + Kind(1) +
//	                                   V(|V_k| if σ-chained, else 0) +
//	                                   Payload(BLS sig or chained IBE ct))
//	NoValueMsg:    ClusterID(32) + OperatorID(8) + Height(8) +
//	               K-1 LayerEntries × (as above)
//	Commit:        ClusterID(32) + OperatorID(8) + Height(8) + Side(1) +
//	               L0Value(|V| or 0) + L0Partial(BLS sig) +
//	               LayerEntries × (only when Side=NRDirect)
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

	valueRootBytes  = 32
	commitSideBytes = 1
	layerKindBytes  = 1
)

func phase1BundleSize(b *twoab.Phase1Bundle) int64 {
	return clusterIDBytes + operatorIDBytes + heightBytes + layerBytes +
		int64(len(b.Value))
}

func layerEntriesSize(entries []twoab.LayerEntry) int64 {
	size := int64(0)
	for _, e := range entries {
		size += layerBytes + layerKindBytes
		size += int64(len(e.V))
		size += int64(len(e.Payload))
	}
	return size
}

func valueMsgSize(v *twoab.ValueMsg) int64 {
	return clusterIDBytes + operatorIDBytes + heightBytes +
		int64(len(v.V)) + valueRootBytes +
		layerEntriesSize(v.LayerEntries)
}

func noValueMsgSize(nv *twoab.NoValueMsg) int64 {
	return clusterIDBytes + operatorIDBytes + heightBytes +
		layerEntriesSize(nv.LayerEntries)
}

func commitSize(c *twoab.Commit) int64 {
	size := int64(clusterIDBytes + operatorIDBytes + heightBytes + commitSideBytes)
	size += int64(len(c.L0Value))
	size += ct.StubSignatureSize // L0Partial
	size += layerEntriesSize(c.LayerEntries)
	return size
}

func certSize(c *twoab.Certificate) int64 {
	return clusterIDBytes + heightBytes + int64(len(c.Value)) + ct.StubSignatureSize
}
