// Package beacontest builds beacon objects for ssvsigner's tests.
package beacontest

import (
	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// InfinitySignature is the compressed point at infinity: a well-formed BLS signature for objects that only
// get hashed, never verified.
var InfinitySignature = phase0.BLSSignature{0xc0}

// GloasBlock returns a minimal self-build Gloas beacon block.
func GloasBlock(slot phase0.Slot, proposerIndex phase0.ValidatorIndex) *gloas.BeaconBlock {
	return &gloas.BeaconBlock{
		Slot:          slot,
		ProposerIndex: proposerIndex,
		ParentRoot:    phase0.Root{0x01},
		StateRoot:     phase0.Root{0x02},
		Body: &gloas.BeaconBlockBody{
			RANDAOReveal: InfinitySignature,
			ETH1Data: &phase0.ETH1Data{
				DepositRoot: phase0.Root{0x01},
				BlockHash:   make([]byte, 32),
			},
			Graffiti: [32]byte{0x01},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits:      bitfield.NewBitvector512(),
				SyncCommitteeSignature: InfinitySignature,
			},
			SignedExecutionPayloadBid: &gloas.SignedExecutionPayloadBid{
				Message: &gloas.ExecutionPayloadBid{
					BuilderIndex: gloas.BuilderIndex(^uint64(0)), // BUILDER_INDEX_SELF_BUILD
					Slot:         slot,
				},
				Signature: InfinitySignature,
			},
			ParentExecutionRequests: &gloas.ExecutionRequests{},
		},
	}
}
