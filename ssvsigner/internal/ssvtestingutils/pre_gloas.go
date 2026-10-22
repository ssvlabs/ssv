package ssvtestingutils

// Pre-Gloas test fixtures. The Gloas-only ssv-spec dropped them from its testingutils, while the signer's
// tests still sign pre-Gloas objects, so they are kept here as the spec last had them. Fixtures the spec
// still has unchanged are used from it.

import (
	"encoding/json"

	"github.com/OffchainLabs/go-bitfield"
	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/gloas"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

const ForkEpochAltair = 74240

const ForkEpochBellatrix = 144896

const ForkEpochFulu = 242000

// ForkEpochPraterCapella Goerli taken from https://github.com/ethereum/execution-specs/blob/37a8f892341eb000e56e962a051a87e05a2e4443/network-upgrades/mainnet-upgrades/shanghai.md?plain=1#L18
const ForkEpochPraterCapella = 162304

const ForkEpochPraterDeneb = 231680

const ForkEpochPraterElectra = 232000

var TestingAttestationData = func(version spec.DataVersion) *phase0.AttestationData {
	attData := &phase0.AttestationData{
		Slot:            TestingDutySlotV(version),
		Index:           spectestingutils.TestingCommitteeIndex,
		BeaconBlockRoot: spectestingutils.TestingBlockRoot,
		Source: &phase0.Checkpoint{
			Epoch: 0,
			Root:  spectestingutils.TestingBlockRoot,
		},
		Target: &phase0.Checkpoint{
			Epoch: 1,
			Root:  spectestingutils.TestingBlockRoot,
		},
	}

	if version >= spec.DataVersionElectra {
		attData.Index = 0
	}
	if version >= gloas.DataVersionGloas {
		attData.Index = 1 // SIP #94 §2: Gloas carries the payload-status index (1 = payload present, matching TestGloasBeaconVote)
	}

	return attData
}

var TestingBeaconBlockCapella = func() *capella.BeaconBlock {
	var res capella.BeaconBlock
	err := json.Unmarshal(capellaBlock, &res)
	if err != nil {
		panic(err)
	}
	// using TestingDutySlotCapella to keep the consistency with TestingProposerDutyV Capella slot
	res.Slot = TestingDutySlotCapella
	return &res
}()

var TestingBeaconBlockV = func(version spec.DataVersion) *api.VersionedProposal {
	switch version {
	case spec.DataVersionCapella:
		return &api.VersionedProposal{
			Version: version,
			Capella: TestingBeaconBlockCapella,
		}
	case spec.DataVersionDeneb:
		return &api.VersionedProposal{
			Version: version,
			Deneb:   TestingBlockContentsDeneb,
		}
	case spec.DataVersionElectra:
		return &api.VersionedProposal{
			Version: version,
			Electra: TestingBlockContentsElectra,
		}
	case spec.DataVersionFulu:
		return &api.VersionedProposal{
			Version: version,
			Fulu:    TestingBlockContentsFulu,
		}
	default:
		panic("unsupported version")
	}
}

var TestingBlindedBeaconBlockCapella = func() *apiv1capella.BlindedBeaconBlock {
	fullBlk := TestingBeaconBlockCapella
	txRoot, _ := types.SSZTransactions(fullBlk.Body.ExecutionPayload.Transactions).HashTreeRoot()
	withdrawalsRoot, _ := types.SSZWithdrawals(fullBlk.Body.ExecutionPayload.Withdrawals).HashTreeRoot()
	ret := &apiv1capella.BlindedBeaconBlock{
		Slot:          fullBlk.Slot,
		ProposerIndex: fullBlk.ProposerIndex,
		ParentRoot:    fullBlk.ParentRoot,
		StateRoot:     fullBlk.StateRoot,
		Body: &apiv1capella.BlindedBeaconBlockBody{
			RANDAOReveal:      fullBlk.Body.RANDAOReveal,
			ETH1Data:          fullBlk.Body.ETH1Data,
			Graffiti:          fullBlk.Body.Graffiti,
			ProposerSlashings: fullBlk.Body.ProposerSlashings,
			AttesterSlashings: fullBlk.Body.AttesterSlashings,
			Attestations:      fullBlk.Body.Attestations,
			Deposits:          fullBlk.Body.Deposits,
			VoluntaryExits:    fullBlk.Body.VoluntaryExits,
			SyncAggregate:     fullBlk.Body.SyncAggregate,
			ExecutionPayloadHeader: &capella.ExecutionPayloadHeader{
				ParentHash:       fullBlk.Body.ExecutionPayload.ParentHash,
				FeeRecipient:     fullBlk.Body.ExecutionPayload.FeeRecipient,
				StateRoot:        fullBlk.Body.ExecutionPayload.StateRoot,
				ReceiptsRoot:     fullBlk.Body.ExecutionPayload.ReceiptsRoot,
				LogsBloom:        fullBlk.Body.ExecutionPayload.LogsBloom,
				PrevRandao:       fullBlk.Body.ExecutionPayload.PrevRandao,
				BlockNumber:      fullBlk.Body.ExecutionPayload.BlockNumber,
				GasLimit:         fullBlk.Body.ExecutionPayload.GasLimit,
				GasUsed:          fullBlk.Body.ExecutionPayload.GasUsed,
				Timestamp:        fullBlk.Body.ExecutionPayload.Timestamp,
				ExtraData:        fullBlk.Body.ExecutionPayload.ExtraData,
				BaseFeePerGas:    fullBlk.Body.ExecutionPayload.BaseFeePerGas,
				BlockHash:        fullBlk.Body.ExecutionPayload.BlockHash,
				TransactionsRoot: txRoot,
				WithdrawalsRoot:  withdrawalsRoot,
			},
			BLSToExecutionChanges: fullBlk.Body.BLSToExecutionChanges,
		},
	}

	return ret
}()

var TestingBlindedBeaconBlockDeneb = func() *apiv1deneb.BlindedBeaconBlock {
	blockContents := TestingBlockContentsDeneb
	txRoot, _ := types.SSZTransactions(blockContents.Block.Body.ExecutionPayload.Transactions).HashTreeRoot()
	withdrawalsRoot, _ := types.SSZWithdrawals(blockContents.Block.Body.ExecutionPayload.Withdrawals).HashTreeRoot()
	ret := &apiv1deneb.BlindedBeaconBlock{
		Slot:          blockContents.Block.Slot,
		ProposerIndex: blockContents.Block.ProposerIndex,
		ParentRoot:    blockContents.Block.ParentRoot,
		StateRoot:     blockContents.Block.StateRoot,
		Body: &apiv1deneb.BlindedBeaconBlockBody{
			RANDAOReveal:      blockContents.Block.Body.RANDAOReveal,
			ETH1Data:          blockContents.Block.Body.ETH1Data,
			Graffiti:          blockContents.Block.Body.Graffiti,
			ProposerSlashings: blockContents.Block.Body.ProposerSlashings,
			AttesterSlashings: blockContents.Block.Body.AttesterSlashings,
			Attestations:      blockContents.Block.Body.Attestations,
			Deposits:          blockContents.Block.Body.Deposits,
			VoluntaryExits:    blockContents.Block.Body.VoluntaryExits,
			SyncAggregate:     blockContents.Block.Body.SyncAggregate,
			ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
				ParentHash:       blockContents.Block.Body.ExecutionPayload.ParentHash,
				FeeRecipient:     blockContents.Block.Body.ExecutionPayload.FeeRecipient,
				StateRoot:        blockContents.Block.Body.ExecutionPayload.StateRoot,
				ReceiptsRoot:     blockContents.Block.Body.ExecutionPayload.ReceiptsRoot,
				LogsBloom:        blockContents.Block.Body.ExecutionPayload.LogsBloom,
				PrevRandao:       blockContents.Block.Body.ExecutionPayload.PrevRandao,
				BlockNumber:      blockContents.Block.Body.ExecutionPayload.BlockNumber,
				GasLimit:         blockContents.Block.Body.ExecutionPayload.GasLimit,
				GasUsed:          blockContents.Block.Body.ExecutionPayload.GasUsed,
				Timestamp:        blockContents.Block.Body.ExecutionPayload.Timestamp,
				ExtraData:        blockContents.Block.Body.ExecutionPayload.ExtraData,
				BaseFeePerGas:    blockContents.Block.Body.ExecutionPayload.BaseFeePerGas,
				BlockHash:        blockContents.Block.Body.ExecutionPayload.BlockHash,
				TransactionsRoot: txRoot,
				WithdrawalsRoot:  withdrawalsRoot,
				BlobGasUsed:      blockContents.Block.Body.ExecutionPayload.BlobGasUsed,
				ExcessBlobGas:    blockContents.Block.Body.ExecutionPayload.ExcessBlobGas,
			},
			BLSToExecutionChanges: blockContents.Block.Body.BLSToExecutionChanges,
			BlobKZGCommitments:    blockContents.Block.Body.BlobKZGCommitments,
		},
	}

	return ret
}()

var TestingBlindedBeaconBlockElectra = func() *apiv1electra.BlindedBeaconBlock {
	blockContents := TestingBlockContentsElectra
	txRoot, _ := types.SSZTransactions(blockContents.Block.Body.ExecutionPayload.Transactions).HashTreeRoot()
	withdrawalsRoot, _ := types.SSZWithdrawals(blockContents.Block.Body.ExecutionPayload.Withdrawals).HashTreeRoot()
	ret := &apiv1electra.BlindedBeaconBlock{
		Slot:          blockContents.Block.Slot,
		ProposerIndex: blockContents.Block.ProposerIndex,
		ParentRoot:    blockContents.Block.ParentRoot,
		StateRoot:     blockContents.Block.StateRoot,
		Body: &apiv1electra.BlindedBeaconBlockBody{
			RANDAOReveal:      blockContents.Block.Body.RANDAOReveal,
			ETH1Data:          blockContents.Block.Body.ETH1Data,
			Graffiti:          blockContents.Block.Body.Graffiti,
			ProposerSlashings: blockContents.Block.Body.ProposerSlashings,
			AttesterSlashings: blockContents.Block.Body.AttesterSlashings,
			Attestations:      blockContents.Block.Body.Attestations,
			Deposits:          blockContents.Block.Body.Deposits,
			VoluntaryExits:    blockContents.Block.Body.VoluntaryExits,
			SyncAggregate:     blockContents.Block.Body.SyncAggregate,
			ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
				ParentHash:       blockContents.Block.Body.ExecutionPayload.ParentHash,
				FeeRecipient:     blockContents.Block.Body.ExecutionPayload.FeeRecipient,
				StateRoot:        blockContents.Block.Body.ExecutionPayload.StateRoot,
				ReceiptsRoot:     blockContents.Block.Body.ExecutionPayload.ReceiptsRoot,
				LogsBloom:        blockContents.Block.Body.ExecutionPayload.LogsBloom,
				PrevRandao:       blockContents.Block.Body.ExecutionPayload.PrevRandao,
				BlockNumber:      blockContents.Block.Body.ExecutionPayload.BlockNumber,
				GasLimit:         blockContents.Block.Body.ExecutionPayload.GasLimit,
				GasUsed:          blockContents.Block.Body.ExecutionPayload.GasUsed,
				Timestamp:        blockContents.Block.Body.ExecutionPayload.Timestamp,
				ExtraData:        blockContents.Block.Body.ExecutionPayload.ExtraData,
				BaseFeePerGas:    blockContents.Block.Body.ExecutionPayload.BaseFeePerGas,
				BlockHash:        blockContents.Block.Body.ExecutionPayload.BlockHash,
				TransactionsRoot: txRoot,
				WithdrawalsRoot:  withdrawalsRoot,
				BlobGasUsed:      blockContents.Block.Body.ExecutionPayload.BlobGasUsed,
				ExcessBlobGas:    blockContents.Block.Body.ExecutionPayload.ExcessBlobGas,
			},
			BLSToExecutionChanges: blockContents.Block.Body.BLSToExecutionChanges,
			BlobKZGCommitments:    blockContents.Block.Body.BlobKZGCommitments,
			ExecutionRequests:     blockContents.Block.Body.ExecutionRequests,
		},
	}

	return ret
}()

var TestingBlindedBeaconBlockFulu = func() *apiv1electra.BlindedBeaconBlock {
	blockContents := TestingBlockContentsFulu
	txRoot, _ := types.SSZTransactions(blockContents.Block.Body.ExecutionPayload.Transactions).HashTreeRoot()
	withdrawalsRoot, _ := types.SSZWithdrawals(blockContents.Block.Body.ExecutionPayload.Withdrawals).HashTreeRoot()
	ret := &apiv1electra.BlindedBeaconBlock{
		Slot:          blockContents.Block.Slot,
		ProposerIndex: blockContents.Block.ProposerIndex,
		ParentRoot:    blockContents.Block.ParentRoot,
		StateRoot:     blockContents.Block.StateRoot,
		Body: &apiv1electra.BlindedBeaconBlockBody{
			RANDAOReveal:      blockContents.Block.Body.RANDAOReveal,
			ETH1Data:          blockContents.Block.Body.ETH1Data,
			Graffiti:          blockContents.Block.Body.Graffiti,
			ProposerSlashings: blockContents.Block.Body.ProposerSlashings,
			AttesterSlashings: blockContents.Block.Body.AttesterSlashings,
			Attestations:      blockContents.Block.Body.Attestations,
			Deposits:          blockContents.Block.Body.Deposits,
			VoluntaryExits:    blockContents.Block.Body.VoluntaryExits,
			SyncAggregate:     blockContents.Block.Body.SyncAggregate,
			ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
				ParentHash:       blockContents.Block.Body.ExecutionPayload.ParentHash,
				FeeRecipient:     blockContents.Block.Body.ExecutionPayload.FeeRecipient,
				StateRoot:        blockContents.Block.Body.ExecutionPayload.StateRoot,
				ReceiptsRoot:     blockContents.Block.Body.ExecutionPayload.ReceiptsRoot,
				LogsBloom:        blockContents.Block.Body.ExecutionPayload.LogsBloom,
				PrevRandao:       blockContents.Block.Body.ExecutionPayload.PrevRandao,
				BlockNumber:      blockContents.Block.Body.ExecutionPayload.BlockNumber,
				GasLimit:         blockContents.Block.Body.ExecutionPayload.GasLimit,
				GasUsed:          blockContents.Block.Body.ExecutionPayload.GasUsed,
				Timestamp:        blockContents.Block.Body.ExecutionPayload.Timestamp,
				ExtraData:        blockContents.Block.Body.ExecutionPayload.ExtraData,
				BaseFeePerGas:    blockContents.Block.Body.ExecutionPayload.BaseFeePerGas,
				BlockHash:        blockContents.Block.Body.ExecutionPayload.BlockHash,
				TransactionsRoot: txRoot,
				WithdrawalsRoot:  withdrawalsRoot,
				BlobGasUsed:      blockContents.Block.Body.ExecutionPayload.BlobGasUsed,
				ExcessBlobGas:    blockContents.Block.Body.ExecutionPayload.ExcessBlobGas,
			},
			BLSToExecutionChanges: blockContents.Block.Body.BLSToExecutionChanges,
			BlobKZGCommitments:    blockContents.Block.Body.BlobKZGCommitments,
			ExecutionRequests:     blockContents.Block.Body.ExecutionRequests,
		},
	}

	return ret
}()

var TestingBlindedBeaconBlockV = func(version spec.DataVersion) *api.VersionedProposal {
	switch version {
	case spec.DataVersionCapella:
		return &api.VersionedProposal{
			Version:        version,
			Blinded:        true,
			CapellaBlinded: TestingBlindedBeaconBlockCapella,
		}
	case spec.DataVersionDeneb:
		return &api.VersionedProposal{
			Version:      version,
			Blinded:      true,
			DenebBlinded: TestingBlindedBeaconBlockDeneb,
		}
	case spec.DataVersionElectra:
		return &api.VersionedProposal{
			Version:        version,
			Blinded:        true,
			ElectraBlinded: TestingBlindedBeaconBlockElectra,
		}
	case spec.DataVersionFulu:
		return &api.VersionedProposal{
			Version:     version,
			Blinded:     true,
			FuluBlinded: TestingBlindedBeaconBlockFulu,
		}
	default:
		panic("unsupported version")
	}
}

var TestingBlockContentsDeneb = func() *apiv1deneb.BlockContents {
	var res apiv1deneb.BlockContents
	if err := json.Unmarshal(denebBlockContents, &res); err != nil {
		panic(err)
	}
	// using ForkEpochPraterDeneb to keep the consistency with TestingProposerDutyV Deneb slot
	res.Block.Slot = ForkEpochPraterDeneb
	return &res
}()

var TestingBlockContentsElectra = func() *apiv1electra.BlockContents {
	var beaconBlock *electra.BeaconBlock
	if err := json.Unmarshal(electraBeaconBlock, &beaconBlock); err != nil {
		panic(err)
	}

	// using ForkEpochPraterElectra to keep the consistency with TestingProposerDutyV Electra slot
	beaconBlock.Slot = ForkEpochPraterElectra

	denebBlockContents := TestingBlockContentsDeneb

	return &apiv1electra.BlockContents{
		Block:     beaconBlock,
		KZGProofs: denebBlockContents.KZGProofs,
		Blobs:     denebBlockContents.Blobs,
	}
}()

var TestingBlockContentsFulu = func() *apiv1fulu.BlockContents {
	var beaconBlock *electra.BeaconBlock
	if err := json.Unmarshal(electraBeaconBlock, &beaconBlock); err != nil {
		panic(err)
	}

	// using ForkEpochFulu to keep the consistency with TestingProposerDutyV Fulu slot
	beaconBlock.Slot = ForkEpochFulu

	denebBlockContents := TestingBlockContentsDeneb

	return &apiv1fulu.BlockContents{
		Block:     beaconBlock,
		KZGProofs: denebBlockContents.KZGProofs,
		Blobs:     denebBlockContents.Blobs,
	}
}()

const TestingDutySlotAltair = ForkEpochAltair * 32

const TestingDutySlotBellatrix = ForkEpochBellatrix * 32

const TestingDutySlotCapella = ForkEpochPraterCapella * 32

const TestingDutySlotDeneb = ForkEpochPraterDeneb * 32

const TestingDutySlotElectra = ForkEpochPraterElectra*32 + 12

const TestingDutySlotFulu = ForkEpochFulu*32 + 12

const TestingDutySlotPhase0 = spectestingutils.TestingDutySlot

var TestingDutySlotV = func(version spec.DataVersion) phase0.Slot {
	switch version {
	case spec.DataVersionPhase0:
		return TestingDutySlotPhase0
	case spec.DataVersionAltair:
		return TestingDutySlotAltair
	case spec.DataVersionBellatrix:
		return TestingDutySlotBellatrix
	case spec.DataVersionCapella:
		return TestingDutySlotCapella
	case spec.DataVersionDeneb:
		return TestingDutySlotDeneb
	case spec.DataVersionElectra:
		return TestingDutySlotElectra
	case spec.DataVersionFulu:
		return TestingDutySlotFulu
	case gloas.DataVersionGloas:
		return spectestingutils.TestingDutySlotGloas

	default:
		panic("unsupported version")
	}
}

var TestingPhase0AggregateAndProof = func(aggregatorIndex phase0.ValidatorIndex) *phase0.AggregateAndProof {
	return &phase0.AggregateAndProof{
		AggregatorIndex: aggregatorIndex,
		SelectionProof:  phase0.BLSSignature{},
		Aggregate: &phase0.Attestation{
			AggregationBits: bitfield.NewBitlist(128),
			Signature:       phase0.BLSSignature{},
			Data:            TestingAttestationData(spec.DataVersionPhase0),
		},
	}
}

// taken from https://github.com/attestantio/go-eth2-client/blob/fa404f5364c5081a7689fe44afb3bb400c9019ba/spec/capella/beaconblock_test.go#L129
var capellaBlock = []byte(`{"slot":"1","proposer_index":"2","parent_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body":{"randao_reveal":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","eth1_data":{"deposit_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","deposit_count":"10","block_hash":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"graffiti":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","proposer_slashings":[{"signed_header_1":{"message":{"slot":"1","proposer_index":"2","parent_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body_root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"},"signed_header_2":{"message":{"slot":"1","proposer_index":"2","parent_root":"0x010102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body_root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}}],"attester_slashings":[{"attestation_1":{"attesting_indices":["1","2","3"],"data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"},"attestation_2":{"attesting_indices":["1","2","3"],"data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}}],"attestations":[{"aggregation_bits":"0x010203","data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}],"deposits":[{"proof":["0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f"],"data":{"pubkey":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f","withdrawal_credentials":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","amount":"32000000000","signature":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f"}}],"voluntary_exits":[{"message":{"epoch":"1","validator_index":"2"},"signature":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}],"sync_aggregate":{"sync_committee_bits":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","sync_committee_signature":"0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f60"},"execution_payload":{"parent_hash":"0x17f4eeae822cc81533016678413443b95e34517e67f12b4a3a92ff6b66f972ef","fee_recipient":"0x58E809C71e4885cB7B3f1D5c793AB04eD239d779","state_root":"0x3d6e230e6eceb8f3db582777b1500b8b31b9d268339e7b32bba8d6f1311b211d","receipts_root":"0xea760203509bdde017a506b12c825976d12b04db7bce9eca9e1ed007056a3f36","logs_bloom":"0x0c803a8d3c6642adee3185bd914c599317d96487831dabda82461f65700b2528781bdadf785664f9d8b11c4ee1139dfeb056125d2abd67e379cabc6d58f1c3ea304b97cf17fcd8a4c53f4dedeaa041acce062fc8fbc88ffc111577db4a936378749f2fd82b4bfcb880821dd5cbefee984bc1ad116096a64a44a2aac8a1791a7ad3a53d91c584ac69a8973daed6daee4432a198c9935fa0e5c2a4a6ca78b821a5b046e571a5c0961f469d40e429066755fec611afe25b560db07f989933556ce0cea4070ca47677b007b4b9857fc092625f82c84526737dc98e173e34fe6e4d0f1a400fd994298b7c2fa8187331c333c415f0499836ff0eed5c762bf570e67b44","prev_randao":"0x76ff751467270668df463600d26dba58297a986e649bac84ea856712d4779c00","block_number":"2983837628677007840","gas_limit":"6738255228996962210","gas_used":"5573520557770513197","timestamp":"1744720080366521389","extra_data":"0xc648","base_fee_per_gas":"88770397543877639215846057887940126737648744594802753726778414602657613619599","block_hash":"0x42c294e902bfc9884c1ce5fef156d4661bb8f0ff488bface37f18c3e7be64b0f","transactions":["0x101b883470f1cb7e0a74561be59e08a6eff6aedd3408c190fd359f0bfb628d2461354b7fe4fdad4b8e72b8775cd44e339ad6b5f22a8f53c418bda2b01200a07f1fe3d010bbb96f5ca0d4919192370c15bc46ed455c797b1b11154be359638f9e487121182fae03a7d26012ed7c85b64a63aa5d56a98ac589f9950a9f5bf1a42c1eea245a98f2f4e743c5f8eac1584893104853dc1b5576826156b371a50c59bcb238d0794a185dc0816dbd8c10a0b0e1b8fbe01c4e8dd719f1e4e9b2ded8613f87f1d01e3ea28e9311d135301b2d1260e4811789e1ecbc33e573346f4da94f4c272e21e23b1d414706429c7f0b40c3de243894349c4a59ece791e5fd086897aef81fc23e4d55bb52b28174c3fa9f2c44d370fbbac1dee561f120560c3dda34a731d4618fd22d595b725efa87bb62f8f93bd7d906c4782a647e2cb14ed293e58bc793852058ab5e6f3df76b30e99102b82c8e7d005e1b675fc74b95032616c590ff08da710dd085be570cc0b2c13891625b44b5b3e1846606c39ad39bb72b","0x4b5edff58cf95969ead0470ab897da6c9d69b517e07fd4ed8fa48c14284a5a060ecb745f66cf1be55fb4905d62d8d376865f7d2d1816844fc4719f5d79ab905474d00f62aed6692e5a93be1b32740a8083fc2a61b0e1fc13ad409410d37f3cb0a5275c966abab015047c15251cd301cd31a7b2a0502f7f953e672d61606616b16d5117163064fb33d97eb566f7fce5f01d3833343c1c97e6221f9f0798415f3a4b87fd472e53a24c1a101e1dd55c8f65c2c0f4ccc4b46a133fda49db5dba96631b4cfd1a05662e42a8e15a26d3148a70be305c85f87dae4217fb91498c4098b946a9042355968b765e2e62bb0cf26d59e534c3af8795fa0f4a44ed0d39d258acd934c3416e4d4a738eaa473526d99bee037765d5f6034c830eb766ef067a1468630fbb65b7c5a862017fe84d4d1961f90c37f18a4bd2509fe2e96cb1e26971900e20295c8a9e9ed77b348d4509a8425090318be5c9d2bcda36bdfabb71bfb36755794f78c877df2825bd736a358933af77eae6edf701ed7ef168f57f677df3445d89c5eefc783184eadd3886fcfd75f5f142bc10904a019acdf7861caa7e0fba3e7831b0a549a56c0f174e80cffb8992346ddf7ce4eeff9e335531df3dff57d5f539bc0d3eac57f70a8f973e0864c87b25c3e0bea72e05eda8a120178186dfbfaa9a00f904f23a","0x452af990975c3bcafee7bde4738fdf32b8479be0e9e30b11b0ccbf31a6f884f4098e47759b7a3eb7d7bd0cfa2510dd654b28d664696aca987f55c7bfe73be1cc70a768cdb2594a13a763dbb991186b8b8b2e913857aadc08f940239d03a0b181ae849d557da5b54bfc966231690bb4660e083cdae28caf8ed33e3f66672772ea827253421bade013af57a290a915dbc777f2afccd9cb29e260ecc5ea54cd3a1e25cf66f2937f8061b3ba6b1ebf3129568f16e3dd04d5c50992cc348f3e615af346dbf2c144aeb19932dcfbff0221fae0ed9706b53245176630d011b9cd2d8630848ab1196cf9a3cc0d94392df4295be246e0ac24545c2715a40dcbc57aabffd0a86acec362affaf1bafc5c75b7ca28698a1ac14ea2c8def8ac1a32d3bf65b98aac7d0cb6fd93e5ff16274ad6d0eedf773694f29fc7a234deebc893e4cea4a5483d876e4f35019d6a62f1c407739b68b7a9f5408b4fb854534344fbffe3239feb17c0e7ac269b447bc6246579e1208b6904751eeacb985cbd43bb7792de0b428f1476301c479a3922f61f650c8298fb0b7584b52c7bfcf4dfe51335ab68d571c5815fc78d772346b0b13dbbb8906f076a0452e7e7a414e005dc37cfee85810eccab5999e2d43e13709bf66e8a8936a4283885b158115050789d3b4d9c8dc8026ac720069d78d47dd5e183032f9c53d4c57640fcd6207118d9738f00cd5ddf587f3a7c401d923aa2fb08dba1768728001abc3436cc2b5cf978b558ae58a0578344e7464cd00e719135c70244e2faf1264571a8999789c26f401753e429a1135f18906ebef19e122489622738724a6424cad363bed43304c1285c8da4824fec75d7c51b0a34070d8b976e8e8c8fd50908a7e440092dddd970fd55793e2a4446342bd3daf5b96220977d8f0c","0x0877448694993717a89c57b63640612ac4b0258cb5fdda4a311650c631c35c7313b6d2a094fdb207857d94500c37ab20ea0aa54af951fb04584a37b857981c6d13922e95cfecb70b69ab9a57ee6c13ccf8aa38c52de008ec16d9090aa4bf15db2f4afcdbb1bf4920efe5a1aeeff2c949d43460d67837af87bcdffd9e972340cb40de6d87fa11d83bbfb29e97ef2509097e8dec69a1318132a5dd7d95c1c1e13cc85d37a33c9f7d52379b4a47bf889903c8f3ebd2800526d0916e1aad00e02b682e55bc2865c3ff4ce0cc6aad1bd7d8e2901ea53f3a5e2c025dbb9a00f0ce88583c1dbd3d491ed04ba8260dc06fdb8a9162e022c75e9f057da0abed537b34214df234e1f8b26e7374cfa5470272ef03f7b41f7ed067c4c6011c8a17b2e65340e36af81ecb86420755fe5a0413495e16fabee3f9e5524ab7b12a3cffe20b1df7be32434d7da3fb1f3e9b16f42a4f550501120036b193701ac9eb6f760c2da70e3175a66b10463e43c0442a56217ca7cd25fdb46f3eff28cf1bdfe1b7eb3bf9e85ad8ffde207529c9bb1094dbae4db04a4bff6571c985bd629cdc2b78f739eca6694f32fded6944202859277740267d5dc4c1f74ddd1401c6d514ce23b885723c4618789b5c2ddeffec2179be8dec1347ca4ebed5e8bb10d7d17d41c9709a978fc6189f0c3d49d1b7f41bf8f1dc112f2ece84b6c6a687f44b95e62d274f89fa07bd0d3fdd3ee2a97233e363329ef7096ae5a45b2982859e983f5d989a928e9b88579308ece2391dbde378b81a54d38b3f81225d59f8bb511ba7eb590154ceb8258b804b6da98b3af6f395f5b3f1d12f5fd3c29ef54f31ccea0a36ec2daec0a87030daba8d079093ddde17871c4aa1a7dc3dbd4d760be2152dc250ca2bba34a55daf257b9e3704c3ee244081524cb1ae7c22a0d22f1c65b88b1e534ea1cb8f75cea4a7c03d6786f85327876da72dff1d4d049b51ecc10124279a0cbc151e76ddd475cf3dfeede59a902f4c7145786b5993c8bf8016265ec36298b27d0c6a21c7484fc01a8f14c8c287d14ab86789e34699fdb57f6c43486f0fd9013f2f2c62c60b75b1dc3e4d38a6f7c06e7029f874a204b059d834ffb44c99f843ae33ed0950","0x259310d5134d22e0ef42c3686b2adadc6cd1ae7d7836ac71a69d8ba2d02d0152c320610c12c57cba182c5d1e21198e787b21d0c522106aa8243ec994c4ea0b7959a3269d13566f3d0a3eb5ed276d9e22b33fc12e26cafde04b24ec0fd90455dc26d30a9fc25588b762681ca69aecc19e7971dd4cd063d4e31ee99f3c82015ed9a70e58b9d7cd9a8de38eba90ceffc629e7d6c06e4f2fe9cd45bae557652fe58cc9be54de9a994bf14c3bce787a106778416fbe966e95e51a35ba78d3dc4e4f7551e2791af00d50362493697d55ea6718ba2f089eda330e26100fb5adbb939afaf74982795414422712e8560cec372eaf1a56b50ba4e00e42b145537e94e88eddd7d200d0153edb6dcc12eb298666b0aef9ea495fccfda06b7affe7227bceb41d9ac9dd150df8642cdb11df5ab92b69629b6a5ccdc7ba92b6cf12172217057d291b2fdc6f104e86617be1b7fffeb36af59a018f50c055e24ef9b6daff839083bc9dfeae9101f6aff49f808f603834802d160283cd71b275bf97eb49f5612215cc8fe93875","0xfa082adb51ff0dca75ff57ba7852d794284db5b8d498002a821e5fc2c57a27cc1fe591f12860895b0057bc75ecbff9824bb46e2af06a785b6adfa9e32f49d6235776c3bcace18b330cec1126ac2bb5f3679339037817eab536fed29fe5c62ef790cb74893f1524280b0111e24fc0172130d00a88361e63511eb56a96e552d02c4944544c193189a152844ca49cadae38b7424426a74d61763716a068ba5ca9f3bdc2e0e9b644f1f2e02596bd3f446bb9f13dfb18ad2c9a2ba97771bb994f801affe2e8dda8d638e366c5b8263cc891a8a35e52c79f0856bfeaf0719bd1bfb54acc467783b6c08e55491d39b20f218342991381c38d357a4659baf8d44ddf5045d7f116bcc55f78c28a0dc100d06dff44e639b8e7adf967816c03de54eccdf9f6402a2806889d2cd980f34e5197771f13f1fa6b8c7c732031664bb675ff12d642ae62459e92b1cc28c62349e1636850b8a3ff411e1f14b097f7fa3b23eaa173d17a13f0703eecd7358aa623057325eed381a2ba13c50a13a3d8adaad0295960d925709d6e7cb101e894d9ae8db3dcb82a45570b6e02ab68aa4ea94f8779a9c45a5599","0x71ea3e9ec7ac8a144753ec38e78401b14b489a79266dd0527a4505adca584fe7406d9f7f05ef46d262384fbf1c0607f745a40681d4855aa34c37375b2cf7c99d46b6ae4e3aa9ee209782dce9d167bbff79686e59426a0513b951818ee02a9ea5ed8bcd0f991f46eee44c6e82fc6d07823a9f44f2fd7bfa0e250d6699cf7ad2577eb9eaf0dd9b1595cf018383c3d45956e508fa982bbb7744522a03bac5cba22f58a41801f579434126f5b866dcc6ca0454e9be1c7f2f6649254f3cc09142068f412d5d454b4e4d5b54e459719550f2df1901a18467d9d5297ed4a9eedb9f402f2bfd4bd2e50ab697ea5bef5e7e8082650b823635a4f0f55c18712d0f4d365824c79827d454425aec0b4ea6561ce3f1c5411ab4dff26e2412791cdaa28bf6c8fa53af412828c599d7876508f78f2c82ee67e8357947c6848af143fc5d20409049925cd1b194244466711ce7c34a72a165392564b96e280406a95da927d14ebbb6b999ef446d5ad49881c219dac8ad02d994e059569e91b84f211a4c39a3fbd594a8c835aa5976e0c8899d81a5abaa14301662b9e14fb96ea89862ad898cd7d77bcd2547f0f40471f86d4a4f26e4274f2e95fd9b2b604de867be95630b1ea6ce45ba79acc81b986605e46acda208bd3302ffcc6e83f91bff8362b3f9641ca0227d8b31341e320003","0x2b2acf1e7e043b2c38ea1750a6c31f174b0a8b00137ec3eea24ed9fdf979bf04ed923a2cf5cc05a9acf697d27ccdb2a903a4846729cd9b2eead414da983d4f5bfd0ff4f2e7eb75988e58c3a635c271","0xce0c3b31143f1d68987deaac86a1e079deb07b3d2d497de5ebe8d94486e9a7b300c691621a68b3af4fee781c7c05a931123f910054d096c2e154950d32a26c38edb70dda50a242be4d15ce60c265767b141011cd84aa585c3af798fc1eb5ea63e93e0a426c3e1468f402f7e64f20281a4bd73cc1234174f1762a9a41989f570997036c885b1fdbf9c8153a1d19afb6526a123ca6a2fe6e98c6009f8439f6b6eea881453cc58ef344338ffd04783ce9c28c2e373812a65157643f679ed99ab35f4024e6ee31877e536b03c38616fccc993365143ddaffce39c805b391674070e993c6464156043a84266860c769de218bb5f5698f4c7a9b74142535cbcc08d5b3f747cbf6a7dfbb6c7b0ccc58af4886bc441558496e9c84d80660117777f01cdb84c0a2d3b0f2d8a6eacff5a0e55bd1be6387c3793ae8f5231c59697ae914894a49b3ae13a3a124cbf6cea33b7eb575cf6cd13e6073ddf4d033a3f87366c27089afc03707ef9a44c828388733d70f09bc08de574d07cf193f0a26a08ce8253bfdb26d5306632012c6dae194783deb31b72ac35bbd57f07f1667746d85f5d5b2132b885f8cb0206949780f9406307396dc7ece7938225fc5a55c65ed3608c27f668ba9b08875979e249655d15a4c3d77b1433ab26f56d80e2ca9178d88501ee682a14d07b17635e9968da2d98aa9eb4a5ae639e42eed0c7735313d3308fd079589d5cb2dd381a9298f67325976a6dcd3e38f836e4bc3106885f6348c57c90beadd7cbca17570b90130aaadc7eb0ed2bfb97c5c8a791d0b6b2736d43b0f444909449e6ea32ba706ddfe28c407a85e82e3f22064696d2fd7b37b33e01e0016ad91047f95f0cf5c5d6abc8fd7698c4155c290a1a1db842e77e656c69ad8bc06cea9e00dadbec886ca64af5fe3045ebc8c549bf23e71c634f02da8b250618c169a54e4af45d799f2de6747bde01395a13f1a605e9be5","0x9ced04a51e77ed0730f3420571164ea5247a670b962ebf6b453660748ca5"],"withdrawals":[{"index":"2","validator_index":"3","address":"0x000102030405060708090a0b0c0d0e0f10111213","amount":"1000000000000000000"}]},"bls_to_execution_changes":[{"message":{"validator_index":"2","from_bls_pubkey":"0xb89bebc699769726a318c8e9971bd3171297c61aea4a6578a7a4f94b547dcba5bac16a89108b6b6a1fe3695d1a874a0b","to_execution_address":"0x000102030405060708090a0b0c0d0e0f10111213"},"signature":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}]}}`)

// taken from "Good" test https://github.com/attestantio/go-eth2-client/blob/fa404f5364c5081a7689fe44afb3bb400c9019ba/api/v1/deneb/blockcontents_test.go
// without its blobs and KZG proofs: the signer's tests use only the block.
var denebBlockContents = []byte(`{"block":{"slot":"1","proposer_index":"2","parent_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body":{"randao_reveal":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","eth1_data":{"deposit_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","deposit_count":"10","block_hash":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"graffiti":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","proposer_slashings":[{"signed_header_1":{"message":{"slot":"1","proposer_index":"2","parent_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body_root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"},"signed_header_2":{"message":{"slot":"1","proposer_index":"2","parent_root":"0x010102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","state_root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","body_root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}}],"attester_slashings":[{"attestation_1":{"attesting_indices":["1","2","3"],"data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"},"attestation_2":{"attesting_indices":["1","2","3"],"data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}}],"attestations":[{"aggregation_bits":"0x010203","data":{"slot":"100","index":"1","beacon_block_root":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","source":{"epoch":"1","root":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"},"target":{"epoch":"2","root":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}},"signature":"0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9fa0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"}],"deposits":[{"proof":["0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f","0x606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f"],"data":{"pubkey":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f","withdrawal_credentials":"0x202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","amount":"32000000000","signature":"0x404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f"}}],"voluntary_exits":[{"message":{"epoch":"1","validator_index":"2"},"signature":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}],"sync_aggregate":{"sync_committee_bits":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f","sync_committee_signature":"0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f60"},"execution_payload":{"parent_hash":"0x17f4eeae822cc81533016678413443b95e34517e67f12b4a3a92ff6b66f972ef","fee_recipient":"0x58E809C71e4885cB7B3f1D5c793AB04eD239d779","state_root":"0x3d6e230e6eceb8f3db582777b1500b8b31b9d268339e7b32bba8d6f1311b211d","receipts_root":"0xea760203509bdde017a506b12c825976d12b04db7bce9eca9e1ed007056a3f36","logs_bloom":"0x0c803a8d3c6642adee3185bd914c599317d96487831dabda82461f65700b2528781bdadf785664f9d8b11c4ee1139dfeb056125d2abd67e379cabc6d58f1c3ea304b97cf17fcd8a4c53f4dedeaa041acce062fc8fbc88ffc111577db4a936378749f2fd82b4bfcb880821dd5cbefee984bc1ad116096a64a44a2aac8a1791a7ad3a53d91c584ac69a8973daed6daee4432a198c9935fa0e5c2a4a6ca78b821a5b046e571a5c0961f469d40e429066755fec611afe25b560db07f989933556ce0cea4070ca47677b007b4b9857fc092625f82c84526737dc98e173e34fe6e4d0f1a400fd994298b7c2fa8187331c333c415f0499836ff0eed5c762bf570e67b44","prev_randao":"0x76ff751467270668df463600d26dba58297a986e649bac84ea856712d4779c00","block_number":"2983837628677007840","gas_limit":"6738255228996962210","gas_used":"5573520557770513197","timestamp":"1744720080366521389","extra_data":"0xc648","base_fee_per_gas":"88770397543877639215846057887940126737648744594802753726778414602657613619599","block_hash":"0x42c294e902bfc9884c1ce5fef156d4661bb8f0ff488bface37f18c3e7be64b0f","transactions":["0x101b883470f1cb7e0a74561be59e08a6eff6aedd3408c190fd359f0bfb628d2461354b7fe4fdad4b8e72b8775cd44e339ad6b5f22a8f53c418bda2b01200a07f1fe3d010bbb96f5ca0d4919192370c15bc46ed455c797b1b11154be359638f9e487121182fae03a7d26012ed7c85b64a63aa5d56a98ac589f9950a9f5bf1a42c1eea245a98f2f4e743c5f8eac1584893104853dc1b5576826156b371a50c59bcb238d0794a185dc0816dbd8c10a0b0e1b8fbe01c4e8dd719f1e4e9b2ded8613f87f1d01e3ea28e9311d135301b2d1260e4811789e1ecbc33e573346f4da94f4c272e21e23b1d414706429c7f0b40c3de243894349c4a59ece791e5fd086897aef81fc23e4d55bb52b28174c3fa9f2c44d370fbbac1dee561f120560c3dda34a731d4618fd22d595b725efa87bb62f8f93bd7d906c4782a647e2cb14ed293e58bc793852058ab5e6f3df76b30e99102b82c8e7d005e1b675fc74b95032616c590ff08da710dd085be570cc0b2c13891625b44b5b3e1846606c39ad39bb72b","0x4b5edff58cf95969ead0470ab897da6c9d69b517e07fd4ed8fa48c14284a5a060ecb745f66cf1be55fb4905d62d8d376865f7d2d1816844fc4719f5d79ab905474d00f62aed6692e5a93be1b32740a8083fc2a61b0e1fc13ad409410d37f3cb0a5275c966abab015047c15251cd301cd31a7b2a0502f7f953e672d61606616b16d5117163064fb33d97eb566f7fce5f01d3833343c1c97e6221f9f0798415f3a4b87fd472e53a24c1a101e1dd55c8f65c2c0f4ccc4b46a133fda49db5dba96631b4cfd1a05662e42a8e15a26d3148a70be305c85f87dae4217fb91498c4098b946a9042355968b765e2e62bb0cf26d59e534c3af8795fa0f4a44ed0d39d258acd934c3416e4d4a738eaa473526d99bee037765d5f6034c830eb766ef067a1468630fbb65b7c5a862017fe84d4d1961f90c37f18a4bd2509fe2e96cb1e26971900e20295c8a9e9ed77b348d4509a8425090318be5c9d2bcda36bdfabb71bfb36755794f78c877df2825bd736a358933af77eae6edf701ed7ef168f57f677df3445d89c5eefc783184eadd3886fcfd75f5f142bc10904a019acdf7861caa7e0fba3e7831b0a549a56c0f174e80cffb8992346ddf7ce4eeff9e335531df3dff57d5f539bc0d3eac57f70a8f973e0864c87b25c3e0bea72e05eda8a120178186dfbfaa9a00f904f23a","0x452af990975c3bcafee7bde4738fdf32b8479be0e9e30b11b0ccbf31a6f884f4098e47759b7a3eb7d7bd0cfa2510dd654b28d664696aca987f55c7bfe73be1cc70a768cdb2594a13a763dbb991186b8b8b2e913857aadc08f940239d03a0b181ae849d557da5b54bfc966231690bb4660e083cdae28caf8ed33e3f66672772ea827253421bade013af57a290a915dbc777f2afccd9cb29e260ecc5ea54cd3a1e25cf66f2937f8061b3ba6b1ebf3129568f16e3dd04d5c50992cc348f3e615af346dbf2c144aeb19932dcfbff0221fae0ed9706b53245176630d011b9cd2d8630848ab1196cf9a3cc0d94392df4295be246e0ac24545c2715a40dcbc57aabffd0a86acec362affaf1bafc5c75b7ca28698a1ac14ea2c8def8ac1a32d3bf65b98aac7d0cb6fd93e5ff16274ad6d0eedf773694f29fc7a234deebc893e4cea4a5483d876e4f35019d6a62f1c407739b68b7a9f5408b4fb854534344fbffe3239feb17c0e7ac269b447bc6246579e1208b6904751eeacb985cbd43bb7792de0b428f1476301c479a3922f61f650c8298fb0b7584b52c7bfcf4dfe51335ab68d571c5815fc78d772346b0b13dbbb8906f076a0452e7e7a414e005dc37cfee85810eccab5999e2d43e13709bf66e8a8936a4283885b158115050789d3b4d9c8dc8026ac720069d78d47dd5e183032f9c53d4c57640fcd6207118d9738f00cd5ddf587f3a7c401d923aa2fb08dba1768728001abc3436cc2b5cf978b558ae58a0578344e7464cd00e719135c70244e2faf1264571a8999789c26f401753e429a1135f18906ebef19e122489622738724a6424cad363bed43304c1285c8da4824fec75d7c51b0a34070d8b976e8e8c8fd50908a7e440092dddd970fd55793e2a4446342bd3daf5b96220977d8f0c","0x0877448694993717a89c57b63640612ac4b0258cb5fdda4a311650c631c35c7313b6d2a094fdb207857d94500c37ab20ea0aa54af951fb04584a37b857981c6d13922e95cfecb70b69ab9a57ee6c13ccf8aa38c52de008ec16d9090aa4bf15db2f4afcdbb1bf4920efe5a1aeeff2c949d43460d67837af87bcdffd9e972340cb40de6d87fa11d83bbfb29e97ef2509097e8dec69a1318132a5dd7d95c1c1e13cc85d37a33c9f7d52379b4a47bf889903c8f3ebd2800526d0916e1aad00e02b682e55bc2865c3ff4ce0cc6aad1bd7d8e2901ea53f3a5e2c025dbb9a00f0ce88583c1dbd3d491ed04ba8260dc06fdb8a9162e022c75e9f057da0abed537b34214df234e1f8b26e7374cfa5470272ef03f7b41f7ed067c4c6011c8a17b2e65340e36af81ecb86420755fe5a0413495e16fabee3f9e5524ab7b12a3cffe20b1df7be32434d7da3fb1f3e9b16f42a4f550501120036b193701ac9eb6f760c2da70e3175a66b10463e43c0442a56217ca7cd25fdb46f3eff28cf1bdfe1b7eb3bf9e85ad8ffde207529c9bb1094dbae4db04a4bff6571c985bd629cdc2b78f739eca6694f32fded6944202859277740267d5dc4c1f74ddd1401c6d514ce23b885723c4618789b5c2ddeffec2179be8dec1347ca4ebed5e8bb10d7d17d41c9709a978fc6189f0c3d49d1b7f41bf8f1dc112f2ece84b6c6a687f44b95e62d274f89fa07bd0d3fdd3ee2a97233e363329ef7096ae5a45b2982859e983f5d989a928e9b88579308ece2391dbde378b81a54d38b3f81225d59f8bb511ba7eb590154ceb8258b804b6da98b3af6f395f5b3f1d12f5fd3c29ef54f31ccea0a36ec2daec0a87030daba8d079093ddde17871c4aa1a7dc3dbd4d760be2152dc250ca2bba34a55daf257b9e3704c3ee244081524cb1ae7c22a0d22f1c65b88b1e534ea1cb8f75cea4a7c03d6786f85327876da72dff1d4d049b51ecc10124279a0cbc151e76ddd475cf3dfeede59a902f4c7145786b5993c8bf8016265ec36298b27d0c6a21c7484fc01a8f14c8c287d14ab86789e34699fdb57f6c43486f0fd9013f2f2c62c60b75b1dc3e4d38a6f7c06e7029f874a204b059d834ffb44c99f843ae33ed0950","0x259310d5134d22e0ef42c3686b2adadc6cd1ae7d7836ac71a69d8ba2d02d0152c320610c12c57cba182c5d1e21198e787b21d0c522106aa8243ec994c4ea0b7959a3269d13566f3d0a3eb5ed276d9e22b33fc12e26cafde04b24ec0fd90455dc26d30a9fc25588b762681ca69aecc19e7971dd4cd063d4e31ee99f3c82015ed9a70e58b9d7cd9a8de38eba90ceffc629e7d6c06e4f2fe9cd45bae557652fe58cc9be54de9a994bf14c3bce787a106778416fbe966e95e51a35ba78d3dc4e4f7551e2791af00d50362493697d55ea6718ba2f089eda330e26100fb5adbb939afaf74982795414422712e8560cec372eaf1a56b50ba4e00e42b145537e94e88eddd7d200d0153edb6dcc12eb298666b0aef9ea495fccfda06b7affe7227bceb41d9ac9dd150df8642cdb11df5ab92b69629b6a5ccdc7ba92b6cf12172217057d291b2fdc6f104e86617be1b7fffeb36af59a018f50c055e24ef9b6daff839083bc9dfeae9101f6aff49f808f603834802d160283cd71b275bf97eb49f5612215cc8fe93875","0xfa082adb51ff0dca75ff57ba7852d794284db5b8d498002a821e5fc2c57a27cc1fe591f12860895b0057bc75ecbff9824bb46e2af06a785b6adfa9e32f49d6235776c3bcace18b330cec1126ac2bb5f3679339037817eab536fed29fe5c62ef790cb74893f1524280b0111e24fc0172130d00a88361e63511eb56a96e552d02c4944544c193189a152844ca49cadae38b7424426a74d61763716a068ba5ca9f3bdc2e0e9b644f1f2e02596bd3f446bb9f13dfb18ad2c9a2ba97771bb994f801affe2e8dda8d638e366c5b8263cc891a8a35e52c79f0856bfeaf0719bd1bfb54acc467783b6c08e55491d39b20f218342991381c38d357a4659baf8d44ddf5045d7f116bcc55f78c28a0dc100d06dff44e639b8e7adf967816c03de54eccdf9f6402a2806889d2cd980f34e5197771f13f1fa6b8c7c732031664bb675ff12d642ae62459e92b1cc28c62349e1636850b8a3ff411e1f14b097f7fa3b23eaa173d17a13f0703eecd7358aa623057325eed381a2ba13c50a13a3d8adaad0295960d925709d6e7cb101e894d9ae8db3dcb82a45570b6e02ab68aa4ea94f8779a9c45a5599","0x71ea3e9ec7ac8a144753ec38e78401b14b489a79266dd0527a4505adca584fe7406d9f7f05ef46d262384fbf1c0607f745a40681d4855aa34c37375b2cf7c99d46b6ae4e3aa9ee209782dce9d167bbff79686e59426a0513b951818ee02a9ea5ed8bcd0f991f46eee44c6e82fc6d07823a9f44f2fd7bfa0e250d6699cf7ad2577eb9eaf0dd9b1595cf018383c3d45956e508fa982bbb7744522a03bac5cba22f58a41801f579434126f5b866dcc6ca0454e9be1c7f2f6649254f3cc09142068f412d5d454b4e4d5b54e459719550f2df1901a18467d9d5297ed4a9eedb9f402f2bfd4bd2e50ab697ea5bef5e7e8082650b823635a4f0f55c18712d0f4d365824c79827d454425aec0b4ea6561ce3f1c5411ab4dff26e2412791cdaa28bf6c8fa53af412828c599d7876508f78f2c82ee67e8357947c6848af143fc5d20409049925cd1b194244466711ce7c34a72a165392564b96e280406a95da927d14ebbb6b999ef446d5ad49881c219dac8ad02d994e059569e91b84f211a4c39a3fbd594a8c835aa5976e0c8899d81a5abaa14301662b9e14fb96ea89862ad898cd7d77bcd2547f0f40471f86d4a4f26e4274f2e95fd9b2b604de867be95630b1ea6ce45ba79acc81b986605e46acda208bd3302ffcc6e83f91bff8362b3f9641ca0227d8b31341e320003","0x2b2acf1e7e043b2c38ea1750a6c31f174b0a8b00137ec3eea24ed9fdf979bf04ed923a2cf5cc05a9acf697d27ccdb2a903a4846729cd9b2eead414da983d4f5bfd0ff4f2e7eb75988e58c3a635c271","0xce0c3b31143f1d68987deaac86a1e079deb07b3d2d497de5ebe8d94486e9a7b300c691621a68b3af4fee781c7c05a931123f910054d096c2e154950d32a26c38edb70dda50a242be4d15ce60c265767b141011cd84aa585c3af798fc1eb5ea63e93e0a426c3e1468f402f7e64f20281a4bd73cc1234174f1762a9a41989f570997036c885b1fdbf9c8153a1d19afb6526a123ca6a2fe6e98c6009f8439f6b6eea881453cc58ef344338ffd04783ce9c28c2e373812a65157643f679ed99ab35f4024e6ee31877e536b03c38616fccc993365143ddaffce39c805b391674070e993c6464156043a84266860c769de218bb5f5698f4c7a9b74142535cbcc08d5b3f747cbf6a7dfbb6c7b0ccc58af4886bc441558496e9c84d80660117777f01cdb84c0a2d3b0f2d8a6eacff5a0e55bd1be6387c3793ae8f5231c59697ae914894a49b3ae13a3a124cbf6cea33b7eb575cf6cd13e6073ddf4d033a3f87366c27089afc03707ef9a44c828388733d70f09bc08de574d07cf193f0a26a08ce8253bfdb26d5306632012c6dae194783deb31b72ac35bbd57f07f1667746d85f5d5b2132b885f8cb0206949780f9406307396dc7ece7938225fc5a55c65ed3608c27f668ba9b08875979e249655d15a4c3d77b1433ab26f56d80e2ca9178d88501ee682a14d07b17635e9968da2d98aa9eb4a5ae639e42eed0c7735313d3308fd079589d5cb2dd381a9298f67325976a6dcd3e38f836e4bc3106885f6348c57c90beadd7cbca17570b90130aaadc7eb0ed2bfb97c5c8a791d0b6b2736d43b0f444909449e6ea32ba706ddfe28c407a85e82e3f22064696d2fd7b37b33e01e0016ad91047f95f0cf5c5d6abc8fd7698c4155c290a1a1db842e77e656c69ad8bc06cea9e00dadbec886ca64af5fe3045ebc8c549bf23e71c634f02da8b250618c169a54e4af45d799f2de6747bde01395a13f1a605e9be5","0x9ced04a51e77ed0730f3420571164ea5247a670b962ebf6b453660748ca5"],"withdrawals":[{"index":"2","validator_index":"3","address":"0x000102030405060708090a0b0c0d0e0f10111213","amount":"1000000000000000000"}],"blob_gas_used":"4438756708366371443","excess_blob_gas":"12504111653614393862"},"bls_to_execution_changes":[{"message":{"validator_index":"2","from_bls_pubkey":"0xb89bebc699769726a318c8e9971bd3171297c61aea4a6578a7a4f94b547dcba5bac16a89108b6b6a1fe3695d1a874a0b","to_execution_address":"0x000102030405060708090a0b0c0d0e0f10111213"},"signature":"0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f"}],"blob_kzg_commitments":["0x95cc5099bbd8420d8ebade383c00a2346dace60a7604f768cd71501757b4d72eeb7d5474a6b615af10379d69aa9f478f","0xae9f2d2217013ef61f995f9074faead9ec24e8048440164ec3d6029b87d43686dd0c97c2df9554fc997d0d66c3a78929"]}},"kzg_proofs":[],"blobs":[]}`)

// Issue: https://github.com/ssvlabs/ssv-spec/issues/521
// Use json string from a test in api/v1/electra of go-eth2-client
// Beacon block taken from https://dora.pectra-devnet-6.ethpandaops.io/slot/58430
// To fill the electra.BlockContents, just get the KZGProofs and Blobs from the deneb block
var electraBeaconBlock = []byte(`{
"slot": "58430",
"proposer_index": "71682",
"parent_root": "0x0a8a09e05b8be4f379eb0c2f87fc2a9ac872bdd68d497a114e2fd877d56868e0",
"state_root": "0x20f3f418181441d26dc2b0dd2768ead0a3595afe71db4f40744e45ac8e59267a",
"body": {
	"randao_reveal": "0xb0c089ebc07e4276f7e91ea10b7fac1c9d1b68a34b47d56f4497fe160aefca67a1e0d06abf094fce6f00107d82e4018501b4cfa4a631ad60a880548e7c51b1f55e77c7b0765fe8d6f03682b11dc7ed1dc0fa98965c1977715c8e57a92502c5cc",
	"eth1_data": {
	"deposit_root": "0xe51ae5cae00e26a0a5c58f885c6ec574315cbe1da7a92368fbb878d9a38f55b1",
	"deposit_count": "5251",
	"block_hash": "0x3c1f0de2241872a48d400f6d04654dd672530e27f2da8e6cad36aea9b79964ce"
	},
	"graffiti": "0x7373762e6e6574776f726b000000000000000000000000000000000000000000",
	"proposer_slashings": [],
	"attester_slashings": [],
	"attestations": [
	{
		"aggregation_bits": "0xdfdfffffff7fffffffefefddfcfffffefffffffffffbfffffffffdfffdfffbfffffffffffffffffffff7dffffffffffffff7fff7fffdfffffffffffdfff7dfffffffffffffdff7ffffd7fd6ffffffffffffffff7bbffffffefffffefffffeffbfdfffffefffffe7ffffffffffffffffffffdfffffffffffffffffdfdeffffffffbfffffdfffffffffffffedfeffffffffbffffffffffeffffffffffffffff7ffffffefed7ffffdffffffffffffffffffffef7fbfffffbdfffffffffffbffffb7fffefffff7ffffffffffeffffff7f7effffefffffffffffffffffffdfdbbfffffffffffefffffffbffffffeffffdfffffbffffffffffffffffffdf7fff7fffffbfffeffffffeffffffffffffdfffffbfdffeffff7fffffff01",
		"data": {
		"slot": "58429",
		"index": "0",
		"beacon_block_root": "0x0a8a09e05b8be4f379eb0c2f87fc2a9ac872bdd68d497a114e2fd877d56868e0",
		"source": {
			"epoch": "1824",
			"root": "0x6566576196ea7da7c1c4663cdd94dddbdbcd036c86ce0976a456458902683ae2"
		},
		"target": {
			"epoch": "1825",
			"root": "0x9ddf3ce96863108c25238d250d651cb69ea765d89b85663633e64a417e0d107c"
		}
		},
		"signature": "0xb3f7d23639124854023d4bb62de9d987595d8927d465374bf62b4df065b90a03c07aabeb615d53cb91fe6521df5df10809e05d513a2506ff799003ce0cd02ed204d2b884c66dcfcb13580c00fac82a749b70be109666c212e1d710aec763577e",
		"committee_bits": "0xffff010000000000"
	},
	{
		"aggregation_bits": "0x0000000000000000000800000000000008",
		"data": {
		"slot": "58429",
		"index": "0",
		"beacon_block_root": "0xbdf72335b18206fb7c671cf5fd6f08e5c41837afdd0e55c225e976c2d74c9dd3",
		"source": {
			"epoch": "1824",
			"root": "0x6566576196ea7da7c1c4663cdd94dddbdbcd036c86ce0976a456458902683ae2"
		},
		"target": {
			"epoch": "1825",
			"root": "0x9ddf3ce96863108c25238d250d651cb69ea765d89b85663633e64a417e0d107c"
		}
		},
		"signature": "0x84599de34546e862b56e235f886d3f814b9cf2b166a515517de256d0f2105476ff17f4e4220f72b2db004fde59895105064d6864b7a32a0be9072bf7aff6123a087dcd369abb986a9750d1409f8731d1a76bafc9101a15a8623d957fa3ae5c49",
		"committee_bits": "0x0000010000000000"
	}
	],
	"deposits": [],
	"voluntary_exits": [],
	"sync_aggregate": {
	"sync_committee_bits": "0xfffffffffffffffbeff7ffffffffffbffbffffff7ffffffffffffffffffffffffeffffefffffffffffffeefffffefffffffbfffffffdfddfffffffffffff7fff",
	"sync_committee_signature": "0x9315b2eb6d4abb5c559631d27ec021a1d8d5e5ec2c31dc73f0efdfdec9b064907af5ab35e39700a0312103b3f050520a041260b08e13a960d612ef5baa2671a80fb68fe61b92cd7d55ffaa9bfc70bec4ef034a3e91a74a68a34e25f7ad57fa52"
	},
	"execution_payload": {
	"parent_hash": "0x0031f1adca11ff9561285640d43a0fb051dd92261a4eb436a992adb442ee5a38",
	"fee_recipient": "0x5cc0dde14e7256340cc820415a6022a7d1c93a35",
	"state_root": "0x2bc6d9bc2b886007a5dca709a2f38677c6fae9c4df57bcd957c1cb758c1f170d",
	"receipts_root": "0x64be3f4b7e3fec53e6c54326207a53ab30e4923603d81a5bb8b68a2129cdcaaa",
	"logs_bloom": "0x00200000000000000000000080000000000000000000100000000000200000000000000000800000000080000000000000000000000010000000000000000000000020000000010001800408000000200000000000000000000000000000000000000000000000000000000000000008000000000000000000000810000040000000000000000000008000000000000000000000000000080000004010000000800000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000001000000000000000000000000000000000010200000000000000000000000000000000000000000000",
	"prev_randao": "0x18d1ed959a38bd14251430545d1627a2bedfb7398e176287da96af31cb1d1af4",
	"block_number": "52220",
	"gas_limit": "30029237",
	"gas_used": "149119",
	"timestamp": "1739305020",
	"extra_data": "0xd883010f00846765746888676f312e32332e36856c696e7578",
	"base_fee_per_gas": "7",
	"block_hash": "0xdea862d95a549dbcd3eeaf163ea9c18180a97fe3cad643fba64993e56cb042ee",
	"transactions": [
		"0x02f9017a8501a5887710822fd885012a05f2008512a05f2000830249f094d27d57804f09a93989e290cf12cb872c39ad2ad280b901040cc7326300000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000bc37ade48e3c40000000000000000000000000000b3db4f6329df01ac317a70200f6614e1cd0db6f7000000000000000000000000fc7360b3b28cf4204268a8354dbec60720d155d200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000c8ae6c2d3f6695e41b5cb149beae76600f4ac97d000000000000000000000000d0ada425f6835193b8507d7de3a77ec1bd6c5377c080a0c7406081f4f7bc260de49ac10c0cc699a7756a4ce486624913714a4ae7e08feaa06617ba22a6adcc5a670e9f7c661fb7093412c2814e7819cbdcb2e94f9720562c",
		"0x03f8988501a58877107984773594008504a817c80082520894559bbf8ca0c7ca02f280792127958ca6ebdea8a48080c08504a817c800e1a0016316f61a259aa607096440fc3eeb90356e079be01975d2fb18347bd50df33c01a0a8852bc4da84a79f14fbce40eab6927f3213bfd612778965a579e0ef4ba5b53ea076bd3aeba730b4fc8aaf055819f3432a2b49fc3ccc6aa1cbfe14117956496b7f",
		"0x03f8988501a58877107784773594008504a817c8008252089445fd980a780887e4fc4ae9f8d042d112d0802ac28080c08504a817c800e1a001aaab2e45067d92fe73be497c6db544c736d11178f08aebaa46f3cf1e5b428280a0e74d1a7eacd977fb3755d8a7f666cac629231acc3f2f9870b03a9d225aeb3b71a05c310f93997dbb3af47e6035c858db08c900afa0546468336fd13efbc2758249"
	],
	"withdrawals": [],
	"blob_gas_used": "262144",
	"excess_blob_gas": "69468160"
	},
	"bls_to_execution_changes": [],
	"blob_kzg_commitments": [
	"0xa95caabd009e189b9f205e0328ff847ad886e4f8e719bd7219875fbb9688fb3fbe7704bb1dfa7e2993a3dea8d0cf767d",
	"0xa90581f0bdb72f608d082931e7fb2f87b2fd4b22d2231cd380beabc0912bd1327d0e100dd2b9d23d861a82e3c7731f3b"
	],
	"execution_requests": {
	"deposits": [],
	"withdrawals": [],
	"consolidations": []
	}
}
}`)
