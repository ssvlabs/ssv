package common

import (
	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"

	"github.com/ssvlabs/ssv/ssvsigner/internal/beacontest"
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

// NewTestBlock creates a standard test beacon block
func NewTestBlock(slot phase0.Slot, proposerIndex phase0.ValidatorIndex) *electra.BeaconBlock {
	return &electra.BeaconBlock{
		Slot:          slot,
		ProposerIndex: proposerIndex,
		ParentRoot:    phase0.Root{0x01},
		StateRoot:     phase0.Root{0x02},
		Body: &electra.BeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{0x01},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{0x01},
				DepositCount: 0,
				BlockHash:    make([]byte, 32),
			},
			Graffiti:          [32]byte{0x01},
			ProposerSlashings: []*phase0.ProposerSlashing{},
			AttesterSlashings: []*electra.AttesterSlashing{},
			Attestations:      []*electra.Attestation{},
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
			ExecutionRequests:     &electra.ExecutionRequests{},
		},
	}
}

// NewTestElectraAggregateAndProof creates an Electra aggregate of one attester in committee 0
func NewTestElectraAggregateAndProof(data *phase0.AttestationData, aggregatorIndex phase0.ValidatorIndex) *electra.AggregateAndProof {
	aggregationBits := bitfield.NewBitlist(8)
	aggregationBits.SetBitAt(0, true)
	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(0, true)

	return &electra.AggregateAndProof{
		AggregatorIndex: aggregatorIndex,
		Aggregate: &electra.Attestation{
			AggregationBits: aggregationBits,
			Data:            data,
			Signature:       beacontest.InfinitySignature,
			CommitteeBits:   committeeBits,
		},
		SelectionProof: beacontest.InfinitySignature,
	}
}

// NewTestGloasAggregateAndProof creates a Gloas aggregate of one attester in committee 0
func NewTestGloasAggregateAndProof(data *phase0.AttestationData, aggregatorIndex phase0.ValidatorIndex) *gloas.AggregateAndProof {
	aggregationBits := bitfield.NewBitlist(8)
	aggregationBits.SetBitAt(0, true)
	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(0, true)

	return &gloas.AggregateAndProof{
		AggregatorIndex: aggregatorIndex,
		Aggregate: &gloas.Attestation{
			AggregationBits: aggregationBits,
			Data:            data,
			Signature:       beacontest.InfinitySignature,
			CommitteeBits:   committeeBits,
		},
		SelectionProof: beacontest.InfinitySignature,
	}
}

// NewTestContributionAndProof creates a sync committee contribution of one member of the subcommittee
func NewTestContributionAndProof(slot phase0.Slot, subcommitteeIndex uint64, aggregatorIndex phase0.ValidatorIndex) *altair.ContributionAndProof {
	aggregationBits := bitfield.NewBitvector128()
	aggregationBits.SetBitAt(0, true)

	return &altair.ContributionAndProof{
		AggregatorIndex: aggregatorIndex,
		Contribution: &altair.SyncCommitteeContribution{
			Slot:              slot,
			BeaconBlockRoot:   phase0.Root{0x01},
			SubcommitteeIndex: subcommitteeIndex,
			AggregationBits:   aggregationBits,
			Signature:         beacontest.InfinitySignature,
		},
		SelectionProof: beacontest.InfinitySignature,
	}
}
