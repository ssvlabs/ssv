package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	bitfield "github.com/prysmaticlabs/go-bitfield"
)

// TestingBeaconBlock returns a minimal self-build Gloas BeaconBlock for the slot, with the required
// fixed-size body fields populated so it round-trips through SSZ. For use in tests.
func TestingBeaconBlock(slot phase0.Slot) *BeaconBlock {
	return &BeaconBlock{
		Slot: slot,
		Body: &BeaconBlockBody{
			ETH1Data:                  &phase0.ETH1Data{BlockHash: make([]byte, 32)},
			SyncAggregate:             &altair.SyncAggregate{SyncCommitteeBits: bitfield.NewBitvector512()},
			SignedExecutionPayloadBid: &SignedExecutionPayloadBid{Message: &ExecutionPayloadBid{BuilderIndex: BuilderIndexSelfBuild}},
			ParentExecutionRequests:   &electra.ExecutionRequests{},
		},
	}
}
