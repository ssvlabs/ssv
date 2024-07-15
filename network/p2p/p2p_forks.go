package p2pv1

// validatorSubnetSubscriptions returns whether the node should subscribe to validator subnets,
// which is true before the committee subnets fork.
func (n *p2pNetwork) validatorSubnetSubscriptions() bool {
	return !n.cfg.Network.CommitteeSubnetsFork()
}
