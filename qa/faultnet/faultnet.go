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
	return n.dispatch(Plan(faults.Active(), msg, slot, n.netCfg.EstimatedCurrentSlot()))
}

func (n *Network) dispatch(out []Outgoing) error {
	for i := range out {
		o := out[i]
		if o.Delay > 0 {
			go func() {
				time.Sleep(o.Delay)
				if err := n.send(o); err != nil {
					n.logger.Warn("qa fault: delayed send failed", zap.Error(err))
				}
			}()
			continue
		}
		if err := n.send(o); err != nil {
			return err
		}
	}
	return nil
}

func (n *Network) send(o Outgoing) error {
	if o.Resign {
		sig, err := n.signer.SignSSVMessage(o.Msg.SSVMessage)
		if err != nil {
			return err
		}
		o.Msg.Signatures = [][]byte{sig}
		o.Msg.OperatorIDs = []spectypes.OperatorID{n.signer.GetOperatorID()}
		faults.Fired(n.logger,
			zap.Uint64("slot", uint64(o.Slot)),
			zap.String("role", o.Msg.SSVMessage.GetID().GetRoleType().String()))
	}
	return n.P2PNetwork.BroadcastAtSlot(o.Msg, o.Slot)
}
