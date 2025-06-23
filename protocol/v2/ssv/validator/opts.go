package validator

import (
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	DefaultQueueSize = 32
)

// Options represents validator-specific options.
type Options struct {
	CommonOptions

	SSVShare    *ssvtypes.SSVShare
	Operator    *spectypes.CommitteeMember
	DutyRunners runner.ValidatorDutyRunners
}

// CommonOptions represents options that all validators share.
type CommonOptions struct {
	NetworkConfig       networkconfig.Network
	Network             specqbft.Network
	Beacon              beacon.BeaconNode
	Storage             *storage.ParticipantStores
	Signer              ekm.BeaconSigner
	OperatorSigner      ssvtypes.OperatorSigner
	DoppelgangerHandler runner.DoppelgangerProvider
	NewDecidedHandler   qbftctrl.NewDecidedHandler
	FullNode            bool
	Exporter            bool
	QueueSize           int
	GasLimit            uint64
	MessageValidator    validation.MessageValidator
	Graffiti            []byte
	ProposerDelay       time.Duration
}

func NewCommonOptions(
	networkConfig networkconfig.Network,
	network specqbft.Network,
	beacon beacon.BeaconNode,
	storage *storage.ParticipantStores,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	doppelgangerHandler runner.DoppelgangerProvider,
	newDecidedHandler qbftctrl.NewDecidedHandler,
	fullNode bool,
	exporter bool,
	historySyncBatchSize int,
	gasLimit uint64,
	messageValidator validation.MessageValidator,
	graffiti []byte,
	proposerDelay time.Duration,
) *CommonOptions {
	result := &CommonOptions{
		NetworkConfig:       networkConfig,
		Network:             network,
		Beacon:              beacon,
		Storage:             storage,
		Signer:              signer,
		OperatorSigner:      operatorSigner,
		DoppelgangerHandler: doppelgangerHandler,
		NewDecidedHandler:   newDecidedHandler,
		FullNode:            fullNode,
		Exporter:            exporter,
		QueueSize:           DefaultQueueSize,
		GasLimit:            gasLimit,
		MessageValidator:    messageValidator,
		Graffiti:            graffiti,
		ProposerDelay:       proposerDelay,
	}

	// If full node, increase the queue size to make enough room for history sync batches to be pushed whole.
	if fullNode {
		result.QueueSize = max(result.QueueSize, historySyncBatchSize*2)
	}

	if result.GasLimit == 0 {
		// TODO: we probably want to define DefaultGasLimit36 const in spectypes package next to DefaultGasLimit,
		// the question is whether 36_000_000 should override the current value of TODO (which is 30_000_000) or
		// we should keep DefaultGasLimit defined as 30_000_000 while introducing new DefaultGasLimit36 = 36_000_000
		// const instead.
		// TODO: Note also, spec-tests rely on this value being set to 30_000_000 (for the runner under test),
		// hence we'll need to address that as well for spec-tests to pass
		defaultGasLimit := uint64(spectypes.DefaultGasLimit)
		if result.NetworkConfig.EstimatedCurrentEpoch() >= result.NetworkConfig.GetGasLimit36Epoch() {
			const DefaultGasLimit36 = uint64(36_000_000)
			defaultGasLimit = DefaultGasLimit36
		}
		result.GasLimit = defaultGasLimit
	}

	return result
}

func (o *CommonOptions) NewOptions(
	share *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	dutyRunners runner.ValidatorDutyRunners,
) *Options {
	return &Options{
		CommonOptions: *o,

		SSVShare:    share,
		Operator:    operator,
		DutyRunners: dutyRunners,
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
