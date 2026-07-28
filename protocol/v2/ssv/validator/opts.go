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
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
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
	Builders            []gloas.BuilderEntry
}

// NewCommonOptions finalizes a CommonOptions literal: it owns QueueSize (any caller-set value is
// overwritten with the default, bumped for full nodes so history-sync batches can be pushed whole).
func NewCommonOptions(opts CommonOptions, historySyncBatchSize int) *CommonOptions {
	opts.QueueSize = defaultValidatorQueueSize
	if opts.FullNode {
		opts.QueueSize = max(opts.QueueSize, historySyncBatchSize*2)
	}
	return &opts
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
