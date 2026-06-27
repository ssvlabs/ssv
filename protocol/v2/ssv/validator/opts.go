package validator

import (
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// defaultValidatorQueueSize is the default capacity of the per-validator-per-role
// message queue and per-committee-per-slot message queue. Observed peak depth in
// production is single-digit messages per queue (see ssv_queue_inbox_size metric);
// 128 keeps a 30x+ headroom while keeping memory footprint bounded. The
// `warnIfInboxIsTooBig` log fires at >50% capacity and will surface any tuning
// regression. Full nodes still bump this to max(default, historySyncBatchSize*2).
const defaultValidatorQueueSize = 128

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
	Network             protocolp2p.Network
	Beacon              beacon.BeaconNode
	Storage             *storage.ParticipantStores
	Signer              ekm.BeaconSigner
	OperatorSigner      ssvtypes.OperatorSigner
	DoppelgangerHandler runner.DoppelgangerProvider
	NewDecidedHandler   qbftctrl.NewDecidedHandler
	FullNode            bool
	ExporterMode        bool
	QueueSize           int
	GasLimit            uint64
	MessageValidator    validation.MessageValidator
	Graffiti            []byte
	ProposerDelay       time.Duration
	ProposerDelayEPBS   time.Duration
}

func NewCommonOptions(
	networkConfig *networkconfig.Network,
	network protocolp2p.Network,
	beacon beacon.BeaconNode,
	storage *storage.ParticipantStores,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	doppelgangerHandler runner.DoppelgangerProvider,
	newDecidedHandler qbftctrl.NewDecidedHandler,
	fullNode bool,
	exporterMode bool,
	historySyncBatchSize int,
	gasLimit uint64,
	messageValidator validation.MessageValidator,
	graffiti []byte,
	proposerDelay time.Duration,
	proposerDelayEPBS time.Duration,
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
		ExporterMode:        exporterMode,
		QueueSize:           defaultValidatorQueueSize,
		GasLimit:            gasLimit,
		MessageValidator:    messageValidator,
		Graffiti:            graffiti,
		ProposerDelay:       proposerDelay,
		ProposerDelayEPBS:   proposerDelayEPBS,
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
