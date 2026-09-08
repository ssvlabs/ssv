package faultnet

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/qa/faults"
)

// Network wraps the node's P2P network. Every method except BroadcastAtSlot is served by the
// embedded interface, so the wrapper satisfies operator/validator.P2PNetwork unchanged.
type Network struct {
	network.P2PNetwork

	signer ssvtypes.OperatorSigner
	netCfg *networkconfig.Network
	logger *zap.Logger
}

// Wrap returns inner unchanged when no fault is active, so a FAULT=none node is byte-for-byte the
// stock node on the wire.
func Wrap(inner network.P2PNetwork, signer ssvtypes.OperatorSigner, netCfg *networkconfig.Network, logger *zap.Logger) network.P2PNetwork {
	if !faults.Enabled() {
		return inner
	}
	return &Network{P2PNetwork: inner, signer: signer, netCfg: netCfg, logger: logger}
}

// Broadcast is deliberately NOT overridden. The stock implementation decodes the message body to
// find its slot (network/p2p/p2p_pubsub.go, broadcastMessageSlot) and no production path calls it —
// every runner and the QBFT instance broadcast through BroadcastAtSlot. Promoting it from the
// embedded interface keeps it byte-identical; a fault that ever needs this path must decode the
// body for the slot, never the wall clock.
func (n *Network) BroadcastAtSlot(msg *spectypes.SignedSSVMessage, slot phase0.Slot) error {
	return n.dispatch(Plan(faults.Active(), msg, slot, n.netCfg.EstimatedCurrentSlot(), n.role7PreForkSlot()))
}

// role7PreForkSlot returns a slot below the Gloas fork boundary, for role7-prefork (MSG-02). Plan
// must stay pure — it never reads a network config — so the decorator computes this here, where
// n.netCfg is already in scope, the same way DelaySlots is turned into a duration below. Computed
// on every broadcast regardless of which fault is active; cheap, and keeps Plan's signature the
// only place that has to know the value exists.
//
// Falls back to slot 0 when there is no scheduled Gloas fork (pre-Gloas network config) or the fork
// is scheduled at epoch 0 (unreachable in practice, but would otherwise underflow the subtraction) —
// either way, 0 is still a slot below any real Gloas fork boundary.
func (n *Network) role7PreForkSlot() phase0.Slot {
	epoch, ok := n.netCfg.GloasForkEpoch()
	if !ok || epoch == 0 {
		return 0
	}
	return phase0.Slot(uint64(epoch)*n.netCfg.SlotsPerEpoch) - 1
}

func (n *Network) dispatch(out []Outgoing) error {
	for i := range out {
		o := out[i]
		// Plan stays pure — it can compute a slot offset (ptc-3-per-epoch) but must never read the
		// clock or a network config to turn that into a duration. The decorator does that
		// conversion here, where n.netCfg is already in scope, and folds it into Delay so the rest
		// of dispatch/sendAsync only ever deals in durations.
		if o.DelaySlots > 0 {
			o.Delay += time.Duration(o.DelaySlots) * n.netCfg.SlotDuration
		}
		if o.Delay > 0 || o.Repeat > 0 {
			go n.sendAsync(o)
			continue
		}
		if err := n.send(o, true); err != nil {
			if !o.Resign {
				// The identity send carries the honest, unmodified message: its failure is a real
				// broadcast failure and must reach the caller, exactly as the stock network would
				// report it.
				return err
			}
			// A forged/resigned send failing must never fail the honest duty it rode alongside —
			// this instrumentation observes the duty, it must not become a dependency of it.
			n.logger.Warn("qa fault: forged send failed, honest broadcast unaffected", zap.Error(err))
		}
	}
	return nil
}

// sendAsync serves the delayed and repeated sends. It is fire-and-forget: a failure is logged, not
// returned, because the caller has already handed off the honest message.
//
// A repeated series (prefs-replay sends up to replayCount+1 copies) announces only its first send;
// intermediate sends stay silent, and one summary line closes the series out, whether it ran to
// completion or was cut short by a send error, so the record shows how many actually went out
// without emitting one Warn line per repeat.
func (n *Network) sendAsync(o Outgoing) {
	if o.Delay > 0 {
		time.Sleep(o.Delay)
	}
	sent := 0
	for i := 0; i <= o.Repeat; i++ {
		if i > 0 && o.Every > 0 {
			time.Sleep(o.Every)
		}
		if i > 0 {
			// SignSSVMessage is deterministic: re-signing byte-identical bytes on every repeat would
			// re-encode to the exact same SignedSSVMessage every time, and gossipsub's own dedup
			// silently drops every send after the first before it ever reaches this node's peers —
			// the series would flood nothing but this node's own "sent" counter. Perturb a byte
			// validation never reads before each repeat past the first (which already carries its
			// own distinct-from-honest perturbation from Plan) so every send in the series is
			// byte-distinct. The signing root is untouched: it is exactly what the fault is testing.
			if err := perturbForRepeat(o.Msg, i); err != nil {
				n.logger.Warn("qa fault: could not perturb repeated send, stopping series", zap.Error(err))
				break
			}
		}
		if err := n.send(o, i == 0); err != nil {
			n.logger.Warn("qa fault: asynchronous send failed", zap.Error(err))
			break
		}
		sent++
	}
	if o.Repeat > 0 {
		n.logger.Warn("🧪 qa fault: repeated send series finished",
			zap.String("qa_fault", string(faults.Active())),
			zap.Int("sent", sent),
			zap.Int("planned", o.Repeat+1))
	}
}

// send re-signs and broadcasts o. announce controls whether a fired Resign send is logged: a
// repeated series wants only its first send announced, with sendAsync's summary line carrying the
// rest; every other caller passes true.
func (n *Network) send(o Outgoing, announce bool) error {
	if o.Resign {
		sig, err := n.signer.SignSSVMessage(o.Msg.SSVMessage)
		if err != nil {
			return err
		}
		o.Msg.Signatures = [][]byte{sig}
		o.Msg.OperatorIDs = []spectypes.OperatorID{n.signer.GetOperatorID()}
		if announce {
			faults.Fired(n.logger,
				zap.Uint64("slot", uint64(o.Slot)),
				zap.String("role", o.Msg.SSVMessage.GetID().GetRoleType().String()))
		}
	}
	return n.P2PNetwork.BroadcastAtSlot(o.Msg, o.Slot)
}
