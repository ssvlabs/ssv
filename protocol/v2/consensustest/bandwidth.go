package consensustest

import (
	"fmt"
	"sort"
	"strings"
)

// BandwidthReport aggregates per-message byte counts emitted during a sim.
// Adapters call Emission once per dispatched message to populate the report;
// honest receivers' counts are tracked symmetrically (sender pays out,
// receiver pays in). Cluster-level totals are derived properties.
//
// In stub-BLS mode, message sizes use BLS-realistic byte counts (see
// stubsig.go) so bandwidth assertions are directly comparable across stub
// and real-BLS modes — only signing/verification cost differs, not wire size.
type BandwidthReport struct {
	TotalBytes     int64
	PerKindBytes   map[string]int64     // MsgKind.String() → bytes
	PerLayerBytes  map[int]int64        // OBFT layer → bytes; -1 = layer-agnostic
	PerOperatorOut map[OperatorID]int64 // bytes each operator emitted
	PerOperatorIn  map[OperatorID]int64 // bytes each operator received
	RejectedBytes  int64                // bytes dropped at validation (byz garbage)
}

// NewBandwidthReport returns a zero-value report with allocated maps.
func NewBandwidthReport() BandwidthReport {
	return BandwidthReport{
		PerKindBytes:   make(map[string]int64),
		PerLayerBytes:  make(map[int]int64),
		PerOperatorOut: make(map[OperatorID]int64),
		PerOperatorIn:  make(map[OperatorID]int64),
	}
}

// Emission records one message of `bytes` bytes from `from` to `to` of `kind`
// at OBFT `layer` (use -1 for layer-agnostic / non-OBFT messages). Both ends
// are charged.
func (b *BandwidthReport) Emission(from, to OperatorID, kind MsgKind, layer int, bytes int64) {
	b.TotalBytes += bytes
	b.PerKindBytes[kind.String()] += bytes
	b.PerLayerBytes[layer] += bytes
	b.PerOperatorOut[from] += bytes
	b.PerOperatorIn[to] += bytes
}

// EmissionToRelay records `bytes` bytes emitted from cluster operator
// `from` to a non-cluster mesh relay peer (DeliveryMesh mode). Mirrors
// Emission for the cluster-wide totals (TotalBytes, PerKindBytes,
// PerLayerBytes, PerOperatorOut[from]) but skips PerOperatorIn —
// relays don't have cluster identity, so charging them to a sentinel
// OperatorID would pollute the per-operator inbound histogram.
// TotalBytes still includes relay-bound bytes because libp2p re-flood
// genuinely puts those bytes on the wire (the cluster ops paid the
// outbound cost), so they're load-bearing for capacity analysis.
func (b *BandwidthReport) EmissionToRelay(from OperatorID, kind MsgKind, layer int, bytes int64) {
	b.TotalBytes += bytes
	b.PerKindBytes[kind.String()] += bytes
	b.PerLayerBytes[layer] += bytes
	b.PerOperatorOut[from] += bytes
}

// EmissionFromRelay records `bytes` bytes delivered by a non-cluster
// mesh relay peer to cluster operator `to`. The mirror of EmissionToRelay:
// the receiving cluster op is charged on PerOperatorIn so its inbound
// histogram captures relay-side reflood reaching the cluster, but
// PerOperatorOut is left unchanged because the relay isn't a cluster
// operator and isn't accounted in per-operator outbound metrics.
// Contributes to TotalBytes — these bytes are real wire traffic.
func (b *BandwidthReport) EmissionFromRelay(to OperatorID, kind MsgKind, layer int, bytes int64) {
	b.TotalBytes += bytes
	b.PerKindBytes[kind.String()] += bytes
	b.PerLayerBytes[layer] += bytes
	b.PerOperatorIn[to] += bytes
}

// EmissionRelayToRelay records `bytes` bytes for a mesh hop between
// two non-cluster relay peers (libp2p re-flood through the subnet
// outside the cluster). Charges TotalBytes / PerKindBytes /
// PerLayerBytes — these bytes hit the wire — but leaves PerOperatorOut
// and PerOperatorIn untouched, since neither endpoint is a cluster
// operator and the cluster's per-op metrics shouldn't attribute relay-
// to-relay flow to anyone.
func (b *BandwidthReport) EmissionRelayToRelay(kind MsgKind, layer int, bytes int64) {
	b.TotalBytes += bytes
	b.PerKindBytes[kind.String()] += bytes
	b.PerLayerBytes[layer] += bytes
}

// Rejection records `bytes` bytes dropped at the validation layer (byz
// garbage that didn't reach the protocol). Counted separately from
// successful emissions; not summed into TotalBytes. Construct the report
// via NewBandwidthReport so the maps are non-nil before calling this.
func (b *BandwidthReport) Rejection(bytes int64) {
	b.RejectedBytes += bytes
}

// SummaryLine returns a one-line human summary suitable for test logs.
// Format: "bw: total=<N>B kind=[<kind>:<bytes> ...] rej=<N>B"
func (b BandwidthReport) SummaryLine() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "bw: total=%s kind=[", humanBytes(b.TotalBytes))
	keys := make([]string, 0, len(b.PerKindBytes))
	for k := range b.PerKindBytes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%s:%s", k, humanBytes(b.PerKindBytes[k]))
	}
	fmt.Fprintf(&sb, "] rej=%s", humanBytes(b.RejectedBytes))
	return sb.String()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
