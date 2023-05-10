package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/ibft/storage"
	qbftctrl "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	DefaultQueueSize = 32
	// DefaultGasLimit sets validator registration gas limit.
	// TODO: This is a reasonable default, but we should probably make this configurable.
	//       Discussion here: https://github.com/ethereum/builder-specs/issues/17
	DefaultGasLimit = 30_000_000
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	Network           specqbft.Network
	Beacon            specssv.BeaconNode
	Storage           *storage.QBFTStores
	SSVShare          *types.SSVShare
	Signer            spectypes.KeyManager
	DutyRunners       runner.DutyRunners
	NewDecidedHandler qbftctrl.NewDecidedHandler
	FullNode          bool
	Exporter          bool
	BuilderProposals  bool
	QueueSize         int
	GasLimit          uint64
}

func (o *Options) defaults() {
	if o.QueueSize == 0 {
		o.QueueSize = DefaultQueueSize
	}
	if o.GasLimit == 0 {
		o.GasLimit = DefaultGasLimit
	}
}

// State of the validator
type State uint32

const (
	// NotStarted the validator hasn't started
	NotStarted State = iota
	// Started validator is running
	Started
)
