package validator

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	DefaultQueueSize = 32
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	NetworkConfig     networkconfig.NetworkConfig
	Network           specqbft.Network
	Beacon            beacon.BeaconNode
	BeaconNetwork     beacon.BeaconNetwork
	Storage           *storage.QBFTStores
	SSVShare          *types.SSVShare
	Operator          *spectypes.Operator
	Signer            spectypes.BeaconSigner
	OperatorSigner    spectypes.OperatorSigner
	SignatureVerifier spectypes.SignatureVerifier
	DutyRunners       runner.ValidatorDutyRunners
	NewDecidedHandler qbftctrl.NewDecidedHandler
	FullNode          bool
	Exporter          bool
	QueueSize         int
	GasLimit          uint64
	MessageValidator  validation.MessageValidator
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
