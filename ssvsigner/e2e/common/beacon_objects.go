package common

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"github.com/prysmaticlabs/go-bitfield"
)

// NewTestAttestationData creates a standard test attestation with the given epochs and slot
func NewTestAttestationData(sourceEpoch, targetEpoch phase0.Epoch, slot phase0.Slot) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            slot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0x01},
		Source: &phase0.Checkpoint{
			Epoch: sourceEpoch,
			Root:  phase0.Root{0x02},
		},
		Target: &phase0.Checkpoint{
			Epoch: targetEpoch,
			Root:  phase0.Root{0x03},
		},
	}
}

// NewTestDenebBlock creates a standard test Deneb beacon block
func NewTestDenebBlock(slot phase0.Slot, proposerIndex phase0.ValidatorIndex) *deneb.BeaconBlock {
	return &deneb.BeaconBlock{
		Slot:          slot,
		ProposerIndex: proposerIndex,
		ParentRoot:    phase0.Root{0x01},
		StateRoot:     phase0.Root{0x02},
		Body: &deneb.BeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{0x01},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{0x01},
				DepositCount: 0,
				BlockHash:    make([]byte, 32),
			},
			Graffiti:          [32]byte{0x01},
			ProposerSlashings: []*phase0.ProposerSlashing{},
			AttesterSlashings: []*phase0.AttesterSlashing{},
			Attestations:      []*phase0.Attestation{},
			Deposits:          []*phase0.Deposit{},
			VoluntaryExits:    []*phase0.SignedVoluntaryExit{},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits:      bitfield.NewBitvector512(),
				SyncCommitteeSignature: phase0.BLSSignature{},
			},
			ExecutionPayload: &deneb.ExecutionPayload{
				ExtraData:     []byte{},
				BaseFeePerGas: &uint256.Int{},
				Transactions:  []bellatrix.Transaction{},
				Withdrawals:   []*capella.Withdrawal{},
			},
			BLSToExecutionChanges: []*capella.SignedBLSToExecutionChange{},
			BlobKZGCommitments:    []deneb.KZGCommitment{},
		},
	}
}
