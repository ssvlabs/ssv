package qbft

import (
	"fmt"
	"slices"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// operatorsCT returns the sim's operator IDs in framework-typed form,
// allocating once per call. Used by the mesh transport path to feed
// recipient-set filters (which key off ct.OperatorID).
func (s *sim) operatorsCT() []ct.OperatorID {
	out := make([]ct.OperatorID, len(s.operators))
	for i, op := range s.operators {
		out[i] = ct.OperatorID(op)
	}
	return out
}

// emitMesh publishes via the per-sim MeshTopology. See the OBFT adapter's
// emitMesh for the design rationale shared across the three protocol
// families. QBFT-specific: the build closure constructs an evtMessageArrival
// carrying the spec-qbft SignedSSVMessage; self-loopback (zero delay) is
// handled by Broadcast directly, not here.
func (s *sim) emitMesh(from ct.OperatorID, kind ct.MsgKind, frameworkRound int, bytes int64, extraDelay time.Duration, recipients []ct.OperatorID, build func(to ct.OperatorID) event) {
	mesh := s.cfg.Mesh
	fromNode := mesh.NodeForOperator(from)
	id := mesh.NewMsgID()
	mesh.MarkSeen(fromNode, id)
	s.cacheArrivalForGossip(fromNode, fromNode, id, kind, frameworkRound, bytes, build)
	fromEP := mesh.EndpointFor(fromNode)
	for _, neighbor := range mesh.Neighbors(fromNode) {
		isProto := mesh.IsProtocol(neighbor)
		if isProto {
			toOp := mesh.OperatorForNode(neighbor)
			// Linear scan beats a map allocate at n ≤ 13.
			if !slices.Contains(recipients, toOp) {
				continue
			}
			if !s.byz.AllowDelivery(from, toOp, kind) {
				continue
			}
		}
		delay := mesh.SampleHopDelay(s.rng, fromEP, mesh.EndpointFor(neighbor), kind)
		mesh.RecordMeshHop(s.cfg.Bandwidth, fromNode, neighbor, kind, frameworkRound, bytes)
		s.schedule(s.now+delay+extraDelay, &evtMeshArrival{
			from:      fromNode,
			to:        neighbor,
			publisher: fromNode,
			msgID:     id,
			kind:      kind,
			round:     frameworkRound,
			bytes:     bytes,
			builder:   build,
		})
	}
}

// cacheArrivalForGossip mirrors the OBFT helper of the same name. See
// protocol/v2/consensustest/obft/des.go cacheArrivalForGossip for the
// rationale; QBFT-specific: `round` plays the role OBFT's `layer`
// does in mesh-hop bandwidth accounting.
func (s *sim) cacheArrivalForGossip(
	cacheOwner, publisher ct.MeshNode,
	msgID ct.MsgID, kind ct.MsgKind, round int, bytes int64,
	builder func(to ct.OperatorID) event,
) {
	mesh := s.cfg.Mesh
	g := mesh.Gossip()
	if !g.Enabled {
		return
	}
	mesh.MCacheInsert(cacheOwner, msgID, ct.MCacheEntry{
		Kind:  kind,
		Bytes: bytes,
		Reinject: func(requester ct.MeshNode) {
			respEP := mesh.EndpointFor(cacheOwner)
			reqEP := mesh.EndpointFor(requester)
			delay := s.cfg.Network.Delay(s.rng, respEP, reqEP, kind)
			mesh.RecordMeshHop(s.cfg.Bandwidth, cacheOwner, requester, kind, round, bytes)
			s.schedule(s.now+delay, &evtMeshArrival{
				from:      cacheOwner,
				to:        requester,
				publisher: publisher,
				msgID:     msgID,
				kind:      kind,
				round:     round,
				bytes:     bytes,
				builder:   builder,
			})
		},
	}, g.HistoryLength)
}

// scheduleInitialHeartbeats mirrors the OBFT helper. See
// protocol/v2/consensustest/obft/des.go scheduleInitialHeartbeats for
// the rationale. Identical body modulo the typed event.
func (s *sim) scheduleInitialHeartbeats() {
	mesh := s.cfg.Mesh
	if mesh == nil {
		return
	}
	g := mesh.Gossip()
	if !g.Enabled || g.HeartbeatInterval <= 0 {
		return
	}
	total := mesh.TotalNodes()
	if total <= 0 {
		return
	}
	phase := g.HeartbeatInterval / time.Duration(total)
	for i := 0; i < total; i++ {
		nodeOffset := time.Duration(i) * phase
		for tick := time.Duration(0); ; tick += g.HeartbeatInterval {
			at := nodeOffset + tick
			if at > s.cfg.RelayCutoff {
				break
			}
			s.schedule(at, &evtMeshHeartbeat{node: ct.MeshNode(i)})
		}
	}
}

// virtualNetwork implements specqbft.Network for one operator. Broadcast
// queues a delivery event for every operator in the cluster (including self —
// the proposer needs its own PROPOSE/PREPARE/COMMIT in its own container for
// quorum to form, mirroring production's runner-side loopback).
type virtualNetwork struct {
	sim *sim
	op  spectypes.OperatorID
}

func newVirtualNetwork(s *sim, op spectypes.OperatorID) *virtualNetwork {
	return &virtualNetwork{sim: s, op: op}
}

func (n *virtualNetwork) Broadcast(_ spectypes.MessageID, msg *spectypes.SignedSSVMessage) error {
	from := ct.OperatorID(n.op)
	kind, round := decodeKindAndRound(msg)
	if n.sim.byz.SuppressBroadcast(from, kind, round) {
		return nil
	}
	msgBytes, encErr := messageWireBytes(msg)
	if encErr != nil && n.sim.cfg.TraceEnabled {
		n.sim.trace = append(n.sim.trace, ct.TraceEntry{
			When:  n.sim.now,
			Event: fmt.Sprintf("MessageEncode-FAILED[from=%d kind=%s err=%v]", from, kind, encErr),
		})
	}
	frameworkRound := frameworkRoundFor(round)
	// Self-delivery (zero delay, no bandwidth) so the broadcaster's own
	// container picks up the message synchronously with peers'. Independent
	// of Direct vs Mesh — the self-feed is a model artifact for state
	// propagation, not a wire event.
	n.sim.schedule(n.sim.now, &evtMessageArrival{
		from: from,
		to:   from,
		msg:  msg.DeepCopy(),
	})
	build := func(to ct.OperatorID) event {
		return &evtMessageArrival{
			from: from,
			to:   to,
			msg:  msg.DeepCopy(),
		}
	}
	if n.sim.cfg.Mesh != nil {
		n.sim.emitMesh(from, kind, frameworkRound, msgBytes, 0, n.sim.operatorsCT(), build)
		return nil
	}
	for _, to := range n.sim.operators {
		toCT := ct.OperatorID(to)
		if toCT == from {
			continue
		}
		if !n.sim.byz.AllowDelivery(from, toCT, kind) {
			continue
		}
		delay := n.sim.byz.OverrideDelay(n.sim.rng, from, toCT, kind)
		if delay < 0 {
			delay = n.sim.cfg.Network.Delay(n.sim.rng, from, toCT, kind)
		}
		if n.sim.cfg.Bandwidth != nil && msgBytes > 0 {
			n.sim.cfg.Bandwidth.Emission(from, toCT, kind, frameworkRound, msgBytes)
		}
		n.sim.schedule(n.sim.now+delay, &evtMessageArrival{
			from: from,
			to:   toCT,
			msg:  msg.DeepCopy(),
		})
	}
	return nil
}

// messageWireBytes returns the wire-byte count for one consensus message,
// counting inner SSVMessage + FullData only. The outer SignedSSVMessage
// wrapper bytes (Signatures + OperatorIDs arrays) are excluded so QBFT's
// per-message accounting is apples-to-apples with the OBFT adapter, which
// counts inner-message bytes only (see consensustest/obft/sizes.go).
// Encode failure is reported via the second return so callers can record a
// trace entry; the byte count omits the inner-encoded portion in that case
// (FullData is still included). Treat any non-nil err as a programmer error
// — Encode failing on a well-formed adapter-built message indicates
// structural corruption upstream.
func messageWireBytes(msg *spectypes.SignedSSVMessage) (int64, error) {
	var n int64
	encoded, err := msg.SSVMessage.Encode()
	if err == nil {
		n = int64(len(encoded))
	}
	n += int64(len(msg.FullData))
	return n, err
}

// postConsensusInnerBytes is the inner-byte count for one operator's
// post-consensus PartialSignatureMessage SSVMessage carrying a single
// partial-sig on the decided value:
//
//	SSVMessage envelope:   MsgType + MsgID(56) + SSZ framing ≈ 65 B
//	PartialSignatureMessages SSZ wrapper:  Type(8) + Slot(8) + list offset + framing ≈ 24 B
//	one PartialSignatureMessage:           PartialSignature(96) + SigningRoot(32) +
//	                                       Signer(8) + ValidatorIndex(8) = 144 B
//
// ≈ 233 B per post-consensus message, inner-only (matches the OBFT adapter's
// inner-byte framing in consensustest/obft/sizes.go). Constant rather than
// a runtime SSZ encode so bandwidth-mode tests don't depend on spec-types'
// encoding stability.
const postConsensusInnerBytes int64 = 233

// frameworkRoundFor maps a QBFT round (1-indexed) to the framework's
// 0-indexed convention used by Bandwidth.PerLayerBytes; round ≤ 0 (undecodable)
// becomes -1 (layer-agnostic).
func frameworkRoundFor(qbftRound int) int {
	if qbftRound <= 0 {
		return -1
	}
	return qbftRound - 1
}

// decodeKindAndRound decodes the inner QBFT message once and extracts both
// the framework MsgKind and the QBFT round. Returns (KindLeaderBroadcast, 0)
// on decode failure (treat malformed inner as leader broadcast — round 0 is
// out-of-range for any legitimate QBFT round so byz hooks no-op).
func decodeKindAndRound(m *spectypes.SignedSSVMessage) (ct.MsgKind, int) {
	pm, err := specqbft.NewProcessingMessage(m)
	if err != nil {
		return ct.KindLeaderBroadcast, 0
	}
	round := int(pm.QBFTMessage.Round)
	switch pm.QBFTMessage.MsgType {
	case specqbft.ProposalMsgType:
		return ct.KindLeaderBroadcast, round
	case specqbft.PrepareMsgType, specqbft.CommitMsgType:
		return ct.KindCommit, round
	case specqbft.RoundChangeMsgType:
		return ct.KindRoundChange, round
	default:
		return ct.KindLeaderBroadcast, round
	}
}
