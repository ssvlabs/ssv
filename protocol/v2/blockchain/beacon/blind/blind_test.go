package blind

import (
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	denebspec "github.com/attestantio/go-eth2-client/spec/deneb"
	electraspec "github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	utilbellatrix "github.com/attestantio/go-eth2-client/util/bellatrix"
	utilcapella "github.com/attestantio/go-eth2-client/util/capella"
	"github.com/holiman/uint256"
	bitfield "github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
)

func makeDenebFullProposal() *api.VersionedProposal {
	// Minimal viable Deneb block with payload, txs and withdrawals so roots are computed.
	txs := []bellatrix.Transaction{bellatrix.Transaction([]byte("tx1")), bellatrix.Transaction([]byte("tx2"))}
	withdrawals := []*capella.Withdrawal{
		{Index: 1, ValidatorIndex: 2, Address: [20]byte{}, Amount: 3},
	}

	payload := &denebspec.ExecutionPayload{
		ParentHash:    [32]byte{1},
		FeeRecipient:  [20]byte{2},
		StateRoot:     [32]byte{3},
		ReceiptsRoot:  [32]byte{4},
		LogsBloom:     [256]byte{},
		PrevRandao:    [32]byte{5},
		BlockNumber:   10,
		GasLimit:      11,
		GasUsed:       12,
		Timestamp:     13,
		ExtraData:     []byte{0xaa, 0xbb},
		BaseFeePerGas: uint256.NewInt(0),
		BlockHash:     [32]byte{6},
		Transactions:  txs,
		Withdrawals:   withdrawals,
		BlobGasUsed:   14,
		ExcessBlobGas: 15,
	}

	sa := &altair.SyncAggregate{}
	sa.SyncCommitteeBits = bitfield.Bitvector512(make([]byte, 64))
	body := &denebspec.BeaconBlockBody{
		RANDAOReveal:          phase0.BLSSignature{},
		ETH1Data:              &phase0.ETH1Data{BlockHash: make([]byte, 32)},
		Graffiti:              [32]byte{7},
		ProposerSlashings:     nil,
		AttesterSlashings:     nil,
		Attestations:          nil,
		Deposits:              nil,
		VoluntaryExits:        nil,
		SyncAggregate:         sa,
		ExecutionPayload:      payload,
		BLSToExecutionChanges: nil,
		BlobKZGCommitments:    nil,
	}

	blk := &denebspec.BeaconBlock{
		Slot:          123,
		ProposerIndex: 42,
		ParentRoot:    [32]byte{8},
		StateRoot:     [32]byte{9},
		Body:          body,
	}

	return &api.VersionedProposal{
		Version: spec.DataVersionDeneb,
		Blinded: false,
		Deneb: &apiv1deneb.BlockContents{
			Block: blk,
		},
	}
}

func TestEnsureBlinded_DenebConvertsAndCopiesFields(t *testing.T) {
	vp := makeDenebFullProposal()

	// Pre-compute expected roots
	txRoot, err := (&utilbellatrix.ExecutionPayloadTransactions{Transactions: vp.Deneb.Block.Body.ExecutionPayload.Transactions}).HashTreeRoot()
	require.NoError(t, err)
	wRoot, err := (&utilcapella.ExecutionPayloadWithdrawals{Withdrawals: vp.Deneb.Block.Body.ExecutionPayload.Withdrawals}).HashTreeRoot()
	require.NoError(t, err)

	blinded, obj, err := EnsureBlinded(vp)
	require.NoError(t, err)
	require.True(t, blinded.Blinded)
	require.NotNil(t, blinded.DenebBlinded)
	require.Nil(t, blinded.Deneb) // we return a blinded proposal variant

	// The object must SSZ marshal (used as QBFT value)
	byts, err := obj.MarshalSSZ()
	require.NoError(t, err)
	require.Greater(t, len(byts), 0)

	// Header fields reflect payload-derived roots
	header := blinded.DenebBlinded.Body.ExecutionPayloadHeader
	require.Equal(t, txRoot, [32]byte(header.TransactionsRoot))
	require.Equal(t, wRoot, [32]byte(header.WithdrawalsRoot))

	// Copy-through for a few consensus fields
	require.Equal(t, vp.Deneb.Block.Slot, blinded.DenebBlinded.Slot)
	require.Equal(t, vp.Deneb.Block.ProposerIndex, blinded.DenebBlinded.ProposerIndex)
	require.Equal(t, vp.Deneb.Block.ParentRoot, blinded.DenebBlinded.ParentRoot)
	require.Equal(t, vp.Deneb.Block.StateRoot, blinded.DenebBlinded.StateRoot)
	require.Equal(t, vp.Deneb.Block.Body.Graffiti, blinded.DenebBlinded.Body.Graffiti)
}

func TestEnsureBlinded_IdempotentOnAlreadyBlinded(t *testing.T) {
	vp := makeDenebFullProposal()
	blinded1, _, err := EnsureBlinded(vp)
	require.NoError(t, err)
	require.True(t, blinded1.Blinded)

	blinded2, _, err := EnsureBlinded(blinded1)
	require.NoError(t, err)
	require.True(t, blinded2.Blinded)
	require.NotNil(t, blinded2.DenebBlinded)
}

func TestEnsureBlinded_CapellaConvertsAndCopiesFields(t *testing.T) {
	// Build a minimal Capella block
	txs := []bellatrix.Transaction{bellatrix.Transaction([]byte("a"))}
	withdrawals := []*capella.Withdrawal{{Index: 1, ValidatorIndex: 2, Amount: 3}}

	payload := &capella.ExecutionPayload{
		ParentHash:    phase0.Hash32{1},
		FeeRecipient:  [20]byte{2},
		StateRoot:     [32]byte{3},
		ReceiptsRoot:  [32]byte{4},
		LogsBloom:     [256]byte{},
		PrevRandao:    [32]byte{5},
		BlockNumber:   10,
		GasLimit:      11,
		GasUsed:       12,
		Timestamp:     13,
		ExtraData:     []byte{0x01},
		BaseFeePerGas: [32]byte{}, // stored LE in header.JSON, ok as zero
		BlockHash:     phase0.Hash32{6},
		Transactions:  txs,
		Withdrawals:   withdrawals,
	}

	sa := &altair.SyncAggregate{}
	sa.SyncCommitteeBits = bitfield.Bitvector512(make([]byte, 64))

	body := &capella.BeaconBlockBody{
		RANDAOReveal:          phase0.BLSSignature{},
		ETH1Data:              &phase0.ETH1Data{BlockHash: make([]byte, 32)},
		Graffiti:              [32]byte{7},
		ProposerSlashings:     nil,
		AttesterSlashings:     nil,
		Attestations:          nil,
		Deposits:              nil,
		VoluntaryExits:        nil,
		SyncAggregate:         sa,
		ExecutionPayload:      payload,
		BLSToExecutionChanges: nil,
	}

	blk := &capella.BeaconBlock{
		Slot:          22,
		ProposerIndex: 33,
		ParentRoot:    [32]byte{8},
		StateRoot:     [32]byte{9},
		Body:          body,
	}

	vp := &api.VersionedProposal{Version: spec.DataVersionCapella, Capella: blk}

	// Expected roots
	txRoot, err := (&utilbellatrix.ExecutionPayloadTransactions{Transactions: payload.Transactions}).HashTreeRoot()
	require.NoError(t, err)
	wRoot, err := (&utilcapella.ExecutionPayloadWithdrawals{Withdrawals: payload.Withdrawals}).HashTreeRoot()
	require.NoError(t, err)

	blinded, obj, err := EnsureBlinded(vp)
	require.NoError(t, err)
	require.True(t, blinded.Blinded)
	require.NotNil(t, blinded.CapellaBlinded)
	_, err = obj.MarshalSSZ()
	require.NoError(t, err)

	hdr := blinded.CapellaBlinded.Body.ExecutionPayloadHeader
	require.Equal(t, txRoot, [32]byte(hdr.TransactionsRoot))
	require.Equal(t, wRoot, [32]byte(hdr.WithdrawalsRoot))
	require.Equal(t, blk.Slot, blinded.CapellaBlinded.Slot)
	require.Equal(t, blk.ProposerIndex, blinded.CapellaBlinded.ProposerIndex)
}

func TestEnsureBlinded_ElectraConverts(t *testing.T) {
	// Minimal Electra block (Electra execution payload layout extends Deneb)
	txs := []bellatrix.Transaction{bellatrix.Transaction([]byte("z"))}
	withdrawals := []*capella.Withdrawal{{Index: 1, ValidatorIndex: 2, Amount: 3}}

	payload := &denebspec.ExecutionPayload{
		ParentHash:    phase0.Hash32{1},
		FeeRecipient:  [20]byte{2},
		StateRoot:     [32]byte{3},
		ReceiptsRoot:  [32]byte{4},
		LogsBloom:     [256]byte{},
		PrevRandao:    [32]byte{5},
		BlockNumber:   10,
		GasLimit:      11,
		GasUsed:       12,
		Timestamp:     13,
		ExtraData:     []byte{0x05},
		BaseFeePerGas: uint256.NewInt(0),
		BlockHash:     phase0.Hash32{6},
		Transactions:  txs,
		Withdrawals:   withdrawals,
		BlobGasUsed:   1,
		ExcessBlobGas: 1,
	}

	sa := &altair.SyncAggregate{}
	sa.SyncCommitteeBits = bitfield.Bitvector512(make([]byte, 64))

	body := &electraspec.BeaconBlockBody{
		RANDAOReveal:          phase0.BLSSignature{},
		ETH1Data:              &phase0.ETH1Data{BlockHash: make([]byte, 32)},
		Graffiti:              [32]byte{7},
		ProposerSlashings:     nil,
		AttesterSlashings:     nil,
		Attestations:          nil,
		Deposits:              nil,
		VoluntaryExits:        nil,
		SyncAggregate:         sa,
		ExecutionPayload:      payload,
		BLSToExecutionChanges: nil,
		BlobKZGCommitments:    nil,
		ExecutionRequests:     &electraspec.ExecutionRequests{},
	}

	blk := &electraspec.BeaconBlock{
		Slot:          77,
		ProposerIndex: 88,
		ParentRoot:    [32]byte{8},
		StateRoot:     [32]byte{9},
		Body:          body,
	}

	vp := &api.VersionedProposal{Version: spec.DataVersionElectra, Electra: &apiv1electra.BlockContents{Block: blk}}

	txRoot, err := (&utilbellatrix.ExecutionPayloadTransactions{Transactions: payload.Transactions}).HashTreeRoot()
	require.NoError(t, err)
	wRoot, err := (&utilcapella.ExecutionPayloadWithdrawals{Withdrawals: payload.Withdrawals}).HashTreeRoot()
	require.NoError(t, err)

	blinded, obj, err := EnsureBlinded(vp)
	require.NoError(t, err)
	require.True(t, blinded.Blinded)
	require.NotNil(t, blinded.ElectraBlinded)
	_, err = obj.MarshalSSZ()
	require.NoError(t, err)

	hdr := blinded.ElectraBlinded.Body.ExecutionPayloadHeader
	require.Equal(t, txRoot, [32]byte(hdr.TransactionsRoot))
	require.Equal(t, wRoot, [32]byte(hdr.WithdrawalsRoot))
}

func TestEnsureBlinded_FuluConverts(t *testing.T) {
	// Fulu reuses Electra block types in this codebase
	txs := []bellatrix.Transaction{bellatrix.Transaction([]byte("q"))}
	withdrawals := []*capella.Withdrawal{{Index: 1, ValidatorIndex: 2, Amount: 3}}

	payload := &denebspec.ExecutionPayload{
		ParentHash:    phase0.Hash32{1},
		FeeRecipient:  [20]byte{2},
		StateRoot:     [32]byte{3},
		ReceiptsRoot:  [32]byte{4},
		LogsBloom:     [256]byte{},
		PrevRandao:    [32]byte{5},
		BlockNumber:   10,
		GasLimit:      11,
		GasUsed:       12,
		Timestamp:     13,
		ExtraData:     []byte{0x05},
		BaseFeePerGas: uint256.NewInt(0),
		BlockHash:     phase0.Hash32{6},
		Transactions:  txs,
		Withdrawals:   withdrawals,
		BlobGasUsed:   1,
		ExcessBlobGas: 1,
	}

	sa := &altair.SyncAggregate{}
	sa.SyncCommitteeBits = bitfield.Bitvector512(make([]byte, 64))

	body := &electraspec.BeaconBlockBody{
		RANDAOReveal:          phase0.BLSSignature{},
		ETH1Data:              &phase0.ETH1Data{BlockHash: make([]byte, 32)},
		Graffiti:              [32]byte{7},
		ProposerSlashings:     nil,
		AttesterSlashings:     nil,
		Attestations:          nil,
		Deposits:              nil,
		VoluntaryExits:        nil,
		SyncAggregate:         sa,
		ExecutionPayload:      payload,
		BLSToExecutionChanges: nil,
		BlobKZGCommitments:    nil,
		ExecutionRequests:     &electraspec.ExecutionRequests{},
	}

	blk := &electraspec.BeaconBlock{
		Slot:          177,
		ProposerIndex: 188,
		ParentRoot:    [32]byte{8},
		StateRoot:     [32]byte{9},
		Body:          body,
	}

	vp := &api.VersionedProposal{Version: spec.DataVersionFulu, Fulu: &apiv1fulu.BlockContents{Block: blk}}

	txRoot, err := (&utilbellatrix.ExecutionPayloadTransactions{Transactions: payload.Transactions}).HashTreeRoot()
	require.NoError(t, err)
	wRoot, err := (&utilcapella.ExecutionPayloadWithdrawals{Withdrawals: payload.Withdrawals}).HashTreeRoot()
	require.NoError(t, err)

	blinded, obj, err := EnsureBlinded(vp)
	require.NoError(t, err)
	require.True(t, blinded.Blinded)
	require.NotNil(t, blinded.FuluBlinded)
	_, err = obj.MarshalSSZ()
	require.NoError(t, err)

	hdr := blinded.FuluBlinded.Body.ExecutionPayloadHeader
	require.Equal(t, txRoot, [32]byte(hdr.TransactionsRoot))
	require.Equal(t, wRoot, [32]byte(hdr.WithdrawalsRoot))
}
