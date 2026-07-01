package gloas

import (
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. -path is this file: sszgen parses the builder requests and
// ExecutionRequests here and resolves the reused Electra request types (deposits/withdrawals/
// consolidations) from the --include path. Includes track go-eth2-client via `go list -m`.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./execution_requests.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/electra --objs BuilderDepositRequest,BuilderExitRequest,ExecutionRequests"

// BuilderDepositRequest is the EIP-8282 builder deposit request — a fixed-size container in the Gloas
// ExecutionRequests.
type BuilderDepositRequest struct {
	Pubkey                phase0.BLSPubKey `ssz-size:"48"`
	WithdrawalCredentials [32]byte         `ssz-size:"32"`
	Amount                phase0.Gwei
	Signature             phase0.BLSSignature `ssz-size:"96"`
}

// BuilderExitRequest is the EIP-8282 builder exit request — a fixed-size container in the Gloas
// ExecutionRequests.
type BuilderExitRequest struct {
	SourceAddress bellatrix.ExecutionAddress `ssz-size:"20"`
	Pubkey        phase0.BLSPubKey           `ssz-size:"48"`
}

// ExecutionRequests is the Gloas execution requests: the Electra three (deposits, withdrawals,
// consolidations) plus the EIP-8282 builder deposit/exit requests. A Gloas CL encodes all five lists, so
// electra.ExecutionRequests (three) marshals a block two offsets short and the CL rejects the §4 submit as
// invalid SSZ — hence this node-side five-list variant. List bounds are the spec MAX_* values.
type ExecutionRequests struct {
	Deposits        []*electra.DepositRequest       `ssz-max:"8192"`
	Withdrawals     []*electra.WithdrawalRequest    `ssz-max:"16"`
	Consolidations  []*electra.ConsolidationRequest `ssz-max:"2"`
	BuilderDeposits []*BuilderDepositRequest        `ssz-max:"256"`
	BuilderExits    []*BuilderExitRequest           `ssz-max:"16"`
}
