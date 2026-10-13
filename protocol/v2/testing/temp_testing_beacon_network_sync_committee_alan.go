//go:build alan_spec

package testing

import "github.com/attestantio/go-eth2-client/spec/phase0"

// SyncCommitteeSubnetID returns the pre-fork identity mapping for Alan fixtures.
func (bn *BeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	return uint64(index)
}
