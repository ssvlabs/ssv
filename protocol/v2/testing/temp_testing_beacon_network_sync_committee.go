//go:build !alan_spec

package testing

import "github.com/attestantio/go-eth2-client/spec/phase0"

// SyncCommitteeSubnetID returns the subnet mapping used by the spec testing beacon node.
func (bn *BeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	return bn.Bn.SyncCommitteeSubnetID(index)
}
