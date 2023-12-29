package peers

type Metrics interface {
	KnownSubnetPeers(subnet, count int)
	ConnectedSubnetPeers(subnet, count int)
	MySubnets(subnet int, existence byte)
}
