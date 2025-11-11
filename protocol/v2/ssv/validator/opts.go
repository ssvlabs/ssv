package validator

import (
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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
	NetworkConfig       *networkconfig.Network
	Network             specqbft.Network
	Beacon              beacon.BeaconNode
	Storage             *storage.ParticipantStores
	Signer              ekm.BeaconSigner
	OperatorSigner      ssvtypes.OperatorSigner
	DoppelgangerHandler runner.DoppelgangerProvider
	NewDecidedHandler   qbftctrl.NewDecidedHandler
	FullNode            bool
	ExporterOptions     exporter.Options
	QueueSize           int
	GasLimit            uint64
	MessageValidator    validation.MessageValidator
	Graffiti            []byte
	ProposerDelay       time.Duration
}

func NewCommonOptions(
	networkConfig *networkconfig.Network,
	network specqbft.Network,
	beacon beacon.BeaconNode,
	storage *storage.ParticipantStores,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	doppelgangerHandler runner.DoppelgangerProvider,
	newDecidedHandler qbftctrl.NewDecidedHandler,
	fullNode bool,
	exporterOptions exporter.Options,
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
		ExporterOptions:     exporterOptions,
		QueueSize:           1000,
		GasLimit:            gasLimit,
		MessageValidator:    messageValidator,
		Graffiti:            graffiti,
		ProposerDelay:       proposerDelay,
	}

	// If full node, increase the queue size to make enough room for history sync batches to be pushed whole.
	if fullNode {
		result.QueueSize = max(result.QueueSize, historySyncBatchSize*2)
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
