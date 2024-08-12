package validator

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ibft/genesisstorage"
	genesisqbftctrl "github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	genesisbeacon "github.com/ssvlabs/ssv/protocol/genesis/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	DefaultQueueSize = 32
)

// Options represents options that should be passed to a new instance of Validator.
type Options struct {
	NetworkConfig     networkconfig.NetworkConfig
	Network           specqbft.Network
	Beacon            beacon.BeaconNode
	GenesisBeacon     genesisbeacon.BeaconNode
	Storage           *storage.QBFTStores
	SSVShare          *ssvtypes.SSVShare
	Operator          *spectypes.CommitteeMember
	Signer            spectypes.BeaconSigner
	OperatorSigner    ssvtypes.OperatorSigner
	DutyRunners       runner.ValidatorDutyRunners
	NewDecidedHandler qbftctrl.NewDecidedHandler
	FullNode          bool
	Exporter          bool
	QueueSize         int
	GasLimit          uint64
	MessageValidator  validation.MessageValidator
	Metrics           Metrics

	GenesisOptions
}

type GenesisOptions struct {
	Network           genesisspecqbft.Network
	Storage           *genesisstorage.QBFTStores
	Signer            genesisspectypes.KeyManager
	NewDecidedHandler genesisqbftctrl.NewDecidedHandler
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
