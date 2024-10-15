package validator

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	genesisibftstorage "github.com/ssvlabs/ssv/ibft/genesisstorage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	genesisbeacon "github.com/ssvlabs/ssv/protocol/genesis/blockchain/beacon"
	qbftctrl "github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

const (
	DefaultQueueSize = 32
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	NetworkConfig     networkconfig.NetworkConfig
	Network           genesisspecqbft.Network
	Beacon            genesisbeacon.BeaconNode
	Storage           *genesisibftstorage.QBFTStores
	SSVShare          *types.SSVShare
	Signer            genesisspectypes.KeyManager
	DutyRunners       runner.DutyRunners
	NewDecidedHandler qbftctrl.NewDecidedHandler
	FullNode          bool
	Exporter          bool
	QueueSize         int
	GasLimit          uint64
	MessageValidator  validation.MessageValidator
	Metrics           Metrics
	Graffiti          []byte
}

func (o *Options) defaults() {
	if o.QueueSize == 0 {
		o.QueueSize = DefaultQueueSize
	}
	if o.GasLimit == 0 {
		o.GasLimit = genesisspectypes.DefaultGasLimit
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
