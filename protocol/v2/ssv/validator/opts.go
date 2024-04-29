package validator

import (
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	specssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/bloxapp/ssv/ibft/storage"
	msgvalidation "github.com/bloxapp/ssv/message/msgvalidation/genesis"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	qbftctrl "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	DefaultQueueSize = 32
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	Network           specqbft.Network
	Beacon            specssv.BeaconNode
	BeaconNetwork     beacon.BeaconNetwork
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
	MessageValidator  msgvalidation.MessageValidator
	Metrics           Metrics
}

func (o *Options) defaults() {
	if o.QueueSize == 0 {
		o.QueueSize = DefaultQueueSize
	}
	if o.GasLimit == 0 {
		o.GasLimit = spectypes.DefaultGasLimit
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
