package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. The includes are resolved from the module graph (`go list -m`)
// so they track go-eth2-client across dependency bumps rather than pinning.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./execution_payload_bid.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/deneb --objs ExecutionPayloadBid,SignedExecutionPayloadBid"

// BuilderIndex identifies a builder in the Gloas builder registry. The sentinel BuilderIndexSelfBuild
// (BUILDER_INDEX_SELF_BUILD = UINT64_MAX) marks a self-built payload — the only path SSV produces.
type BuilderIndex uint64

// BuilderIndexSelfBuild (BUILDER_INDEX_SELF_BUILD) flags a self-built execution payload (SIP #94 §4).
const BuilderIndexSelfBuild = BuilderIndex(^uint64(0))

// ExecutionPayloadBid is the Gloas (ePBS) bid the proposer commits to in the block body, replacing the
// inline execution payload of pre-Gloas blocks (consensus-specs gloas): the block carries only this
// commitment, and the payload itself ships separately in the envelope (§6). For self-build, BuilderIndex
// is BuilderIndexSelfBuild. Fields match the pinned spec / go-eth2-client PR #280 (the earlier #269
// shape — a single BlobKZGCommitmentsRoot — predates the blob-commitments-list change and is stale).
type ExecutionPayloadBid struct {
	ParentBlockHash       phase0.Hash32              `ssz-size:"32"`
	ParentBlockRoot       phase0.Root                `ssz-size:"32"`
	BlockHash             phase0.Hash32              `ssz-size:"32"`
	PrevRandao            phase0.Root                `ssz-size:"32"`
	FeeRecipient          bellatrix.ExecutionAddress `ssz-size:"20"`
	GasLimit              uint64
	BuilderIndex          BuilderIndex
	Slot                  phase0.Slot
	Value                 phase0.Gwei
	ExecutionPayment      phase0.Gwei
	BlobKZGCommitments    []deneb.KZGCommitment `ssz-max:"4096" ssz-size:"?,48"`
	ExecutionRequestsRoot phase0.Root           `ssz-size:"32"`
}

// SignedExecutionPayloadBid wraps an ExecutionPayloadBid with the builder's (or, for self-build, the
// proposer's) signature.
type SignedExecutionPayloadBid struct {
	Message   *ExecutionPayloadBid
	Signature phase0.BLSSignature `ssz-size:"96"`
}
