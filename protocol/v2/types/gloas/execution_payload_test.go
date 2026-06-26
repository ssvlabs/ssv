package gloas

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// TestExecutionPayloadLayoutMatchesSpec pins the hash-tree root of a fully-populated ExecutionPayload,
// guarding the SSZ field order and bounds that were verified against the canonical Gloas container
// (consensus-specs specs/gloas/beacon-chain.md at 6ebb2216c). Each field carries a distinct value, so any
// reorder, type change, or bound change moves the root. If this fails after an intentional struct change,
// re-verify the layout against the spec before updating the golden value.
func TestExecutionPayloadLayoutMatchesSpec(t *testing.T) {
	payload := &ExecutionPayload{
		ParentHash:      phase0.Hash32{0x01},
		FeeRecipient:    bellatrix.ExecutionAddress{0x02},
		StateRoot:       phase0.Root{0x03},
		ReceiptsRoot:    phase0.Root{0x04},
		LogsBloom:       [256]byte{0x05},
		PrevRandao:      phase0.Hash32{0x06},
		BlockNumber:     7,
		GasLimit:        8,
		GasUsed:         9,
		Timestamp:       10,
		ExtraData:       []byte{0x0b},
		BaseFeePerGas:   [32]byte{0x0c},
		BlockHash:       phase0.Hash32{0x0d},
		Transactions:    []bellatrix.Transaction{{0x0e}},
		Withdrawals:     []*capella.Withdrawal{{Index: 0x0f}},
		BlobGasUsed:     16,
		ExcessBlobGas:   17,
		BlockAccessList: []byte{0x12},
		SlotNumber:      19,
	}

	root, err := payload.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, "6db7211d8fd726d055ee254300728ecec0dc4c1dd02616037621c7d8f4d4e5fb", hex.EncodeToString(root[:]))
}
