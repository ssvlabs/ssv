package psigs

import (
	"fmt"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
)

// pSigBytes is the wire size of a single partial-sig message for
// bandwidth accounting. Models a BLS partial sig (96 bytes) + minimal
// envelope wrapping (slot, op-id, message type, op signature ≈ 160
// bytes). Matches the order of magnitude of QBFT's post-consensus
// partial-sig wire footprint.
const pSigBytes int64 = 96 + 160

// ---- evtPSigSign: operator signs V at BFTStart and broadcasts --------

// evtPSigSign fires once per honest operator at BFTStart. The op self-
// observes (count = 1) and broadcasts the partial sig to every peer.
// The local self-observation can already satisfy qV at f=0 (degenerate
// n=1 single-op "cluster" where qV=1) — we record the decision time
// here to cover that edge case; for realistic f≥1 the count increments
// via evtPSigArrival as peers' partials land.
type evtPSigSign struct {
	op ct.OperatorID
}

func (e *evtPSigSign) Describe() string {
	return fmt.Sprintf("PSigSign[op=%d]", e.op)
}

func (e *evtPSigSign) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	s.partialCount[e.op] = 1
	if s.partialCount[e.op] >= s.threshold {
		s.decidedAt[e.op] = s.Now()
	}
	// Byz patterns can delay an operator's own broadcast (ByzDelayedCommit)
	// so the partial-sig lands at peers past the soft target. extraDelay is
	// added on top of the per-pair network delay.
	extraDelay := s.cfg.Byz.OverrideOwnSignDispatchDelay(e.op)
	s.emitToAll(e.op, ct.KindPostConsensus, pSigBytes, extraDelay, func(to ct.OperatorID) desim.Event {
		return &evtPSigArrival{from: e.op, to: to}
	})
	return nil
}

// ---- evtPSigArrival: a peer's partial sig lands at the receiver -------

type evtPSigArrival struct {
	from, to ct.OperatorID
}

func (e *evtPSigArrival) Describe() string {
	return fmt.Sprintf("PSigArrival[from=%d to=%d]", e.from, e.to)
}

func (e *evtPSigArrival) Handle(h desim.Host) []desim.Scheduled {
	s := h.(*sim)
	// Early-exit once the receiver has already reached qV — additional
	// partials don't change the decided time, and skipping them keeps the
	// partialCount bounded by qV. Mesh re-flood is already dedup'd by
	// MeshTopology.MarkSeen in the shared mesh transport, and direct mode
	// schedules each (from, to) exactly once, so this guard is purely an
	// optimization (no risk of double-counting a single partial).
	if _, already := s.decidedAt[e.to]; already {
		return nil
	}
	s.partialCount[e.to]++
	if s.partialCount[e.to] >= s.threshold {
		s.decidedAt[e.to] = s.Now()
	}
	return nil
}
