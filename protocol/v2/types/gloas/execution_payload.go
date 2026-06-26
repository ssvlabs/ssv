package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. -path is the package dir so sszgen resolves the sibling gloas
// types; includes track go-eth2-client via `go list -m`.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path . --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/capella --objs ExecutionPayload --output ./execution_payload_encoding.go"

// ExecutionPayload is the Gloas (ePBS) execution payload — Deneb/Electra's payload plus the two
// Glamsterdam additions: BlockAccessList (EIP-7928, an opaque RLP-encoded byte list the consensus layer
// only stores and hashes) and SlotNumber (EIP-7843). It ships in the §6 ExecutionPayloadEnvelope, not
// inline in the block. BaseFeePerGas is the 32-byte little-endian SSZ form of the spec's uint256
// (HTR-identical).
//
// TODO(gloas §6): confirm the field order and the BlockAccessList ssz-max bound against canonical spec
// test vectors (the §6 HTR-parity test) before relying on the payload root on a live network.
type ExecutionPayload struct {
	ParentHash      phase0.Hash32              `ssz-size:"32"`
	FeeRecipient    bellatrix.ExecutionAddress `ssz-size:"20"`
	StateRoot       phase0.Root                `ssz-size:"32"`
	ReceiptsRoot    phase0.Root                `ssz-size:"32"`
	LogsBloom       [256]byte                  `ssz-size:"256"`
	PrevRandao      phase0.Hash32              `ssz-size:"32"`
	BlockNumber     uint64
	GasLimit        uint64
	GasUsed         uint64
	Timestamp       uint64
	ExtraData       []byte                  `ssz-max:"32"`
	BaseFeePerGas   [32]byte                `ssz-size:"32"`
	BlockHash       phase0.Hash32           `ssz-size:"32"`
	Transactions    []bellatrix.Transaction `ssz-max:"1048576,1073741824" ssz-size:"?,?"`
	Withdrawals     []*capella.Withdrawal   `ssz-max:"16"`
	BlobGasUsed     uint64
	ExcessBlobGas   uint64
	BlockAccessList []byte `ssz-max:"1073741824"`
	SlotNumber      uint64
}
