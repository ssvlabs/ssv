package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

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
	// Wire size accounting: encode the inner SSVMessage and add per-signer
	// (signature + operator-ID) plus FullData. If encode fails (programming
	// error), still account for the independently-knowable sigs/IDs/FullData
	// — a malformed message still occupies wire bytes in practice.
	var msgBytes int64
	if encoded, err := msg.SSVMessage.Encode(); err == nil {
		msgBytes = int64(len(encoded))
	}
	for i := range msg.Signatures {
		msgBytes += int64(len(msg.Signatures[i])) + 8
	}
	msgBytes += int64(len(msg.FullData))
	// Framework round = QBFT round - 1 (0-indexed), or -1 if undecodable.
	frameworkRound := round - 1
	if round <= 0 {
		frameworkRound = -1
	}
	for _, to := range n.sim.operators {
		toCT := ct.OperatorID(to)
		// Self-delivery (zero delay) so the broadcaster's own container
		// picks up the message synchronously with peers'. NOT counted in
		// bandwidth — production gossipsub doesn't loopback per-message;
		// self-feed is a model artifact for state propagation.
		if toCT == from {
			n.sim.schedule(n.sim.now, &evtMessageArrival{
				from: from,
				to:   toCT,
				msg:  msg.DeepCopy(),
			})
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
