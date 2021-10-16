package controller

import (
	"github.com/bloxapp/ssv/ibft"
	contollerforks "github.com/bloxapp/ssv/ibft/controller/forks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"sync"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
)

// Controller implements Controller interface
type Controller struct {
	currentInstance ibft.Instance
	logger          *zap.Logger
	ibftStorage     collections.Iibft
	network         network.Network
	msgQueue        *msgqueue.MessageQueue
	instanceConfig  *proto.InstanceConfig
	ValidatorShare  *storage.Share
	Identifier      []byte
	fork            contollerforks.Fork
	signer          ibft.Signer

	// flags
	initFinished bool

	// locks
	currentInstanceLock sync.Locker
	syncingLock         *semaphore.Weighted
}

// New is the constructor of Controller
func New(
	role beacon.RoleType,
	identifier []byte,
	logger *zap.Logger,
	storage collections.Iibft,
	network network.Network,
	queue *msgqueue.MessageQueue,
	instanceConfig *proto.InstanceConfig,
	ValidatorShare *storage.Share,
	fork contollerforks.Fork,
	signer ibft.Signer,
) ibft.Controller {
	logger = logger.With(zap.String("role", role.String()))
	ret := &Controller{
		ibftStorage:    storage,
		logger:         logger,
		network:        network,
		msgQueue:       queue,
		instanceConfig: instanceConfig,
		ValidatorShare: ValidatorShare,
		Identifier:     identifier,
		signer:         signer,

		// flags
		initFinished: false,

		// locks
		currentInstanceLock: &sync.Mutex{},
		syncingLock:         semaphore.NewWeighted(1),
	}

	ret.setFork(fork)

	return ret
}

// Init sets all major processes of iBFT while blocking until completed.
func (i *Controller) Init() error {
	i.logger.Info("iBFT implementation init started")
	i.processDecidedQueueMessages()
	i.processSyncQueueMessages()
	i.listenToSyncMessages()
	i.listenToNetworkMessages()
	i.listenToNetworkDecidedMessages()
	i.waitForMinPeerOnInit(1) // minimum of 2 validators (me + 1)
	if err := i.SyncIBFT(); err != nil {
		return errors.Wrap(err, "could not sync history, stopping Controller init")
	}
	i.initFinished = true
	i.logger.Info("iBFT implementation init finished")
	return nil
}

// StartInstance - starts an ibft instance or returns error
func (i *Controller) StartInstance(opts ibft.ControllerStartInstanceOptions) (*ibft.InstanceResult, error) {
	instanceOpts, err := i.instanceOptionsFromStartOptions(opts)
	if err != nil {
		return nil, errors.WithMessage(err, "can't generate instance options")
	}

	if err := i.canStartNewInstance(*instanceOpts); err != nil {
		return nil, errors.WithMessage(err, "can't start new iBFT instance")
	}

	return i.startInstanceWithOptions(instanceOpts, opts.Value)
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (i *Controller) GetIBFTCommittee() map[uint64]*proto.Node {
	return i.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (i *Controller) GetIdentifier() []byte {
	return i.Identifier //TODO should use mutex to lock var?
}

// setFork sets Controller fork for any new instances
func (i *Controller) setFork(fork contollerforks.Fork) {
	if fork == nil {
		return
	}
	i.fork = fork
	i.fork.Apply(i)
}
