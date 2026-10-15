package p2pv1

import (
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

// Friends-mode dKMS messages belong to a key, not to a validator share, so they can't be routed by
// BroadcastAtSlot. A key's committee publishes on the Boole committee topic of its operators, the
// same topic a validator cluster of those operators would use.

// SubscribeOperators subscribes to the committee topic of the given operators and keeps it
// subscribed (as a fixed subnet).
func (n *p2pNetwork) SubscribeOperators(operatorIDs []spectypes.OperatorID) error {
	subnet := commons.BooleCommitteeSubnet(operatorIDs)
	if subnet == commons.UnknownSubnetId {
		return fmt.Errorf("no operators")
	}
	n.persistentSubnets.Set(subnet)
	return n.subscribeSubnet(subnet, true)
}

// BroadcastToOperators publishes a message on the committee topic of the given operators.
func (n *p2pNetwork) BroadcastToOperators(operatorIDs []spectypes.OperatorID, msg *spectypes.SignedSSVMessage) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	subnet := commons.BooleCommitteeSubnet(operatorIDs)
	if subnet == commons.UnknownSubnetId {
		return fmt.Errorf("no operators")
	}
	encoded, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode signed ssv message: %w", err)
	}
	topic := commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, subnet)
	if err := n.topicsCtrl.Broadcast(topic, encoded, n.cfg.RequestTimeout); err != nil {
		return fmt.Errorf("could not broadcast msg: %w", err)
	}
	return nil
}
