package controller

import (
	"github.com/bloxapp/ssv/ibft"
	contollerforks "github.com/bloxapp/ssv/ibft/controller/forks"
	"github.com/bloxapp/ssv/utils/threadsafe"
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

var (
	// ErrAlreadyRunning is used to express that some process is already running, e.g. sync
	ErrAlreadyRunning = errors.New("already running")
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
	signer          beacon.Signer

	// flags
	initHandlers *threadsafe.SafeBool
	initSynced   *threadsafe.SafeBool

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
	signer beacon.Signer,
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
		initHandlers: threadsafe.NewSafeBool(),
		initSynced:   threadsafe.NewSafeBool(),

		// locks
		currentInstanceLock: &sync.Mutex{},
		syncingLock:         semaphore.NewWeighted(1),
	}

	ret.setFork(fork)

	return ret
}

// Init sets all major processes of iBFT while blocking until completed.
// if init fails to sync
func (i *Controller) Init() error {
	if !i.initHandlers.Get() {
		i.logger.Info("iBFT implementation init started")
		ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		i.processDecidedQueueMessages()
		i.processSyncQueueMessages()
		i.listenToSyncMessages()
		i.listenToNetworkMessages()
		i.listenToNetworkDecidedMessages()
		i.initHandlers.Set(true)
		i.logger.Debug("iBFT handlers-setup finished")
	}

	if !i.initSynced.Get() {
		// IBFT sync to make sure the operator is aligned for this validator
		// if fails - controller needs to be initialized again, otherwise it will be unavailable
		if err := i.SyncIBFT(); err != nil {
			if err == ErrAlreadyRunning {
				// don't fail if init is already running
				i.logger.Debug("iBFT init is already running (syncing history)")
				return nil
			}
			i.logger.Warn("iBFT implementation init failed to sync history")
			ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), false, true)
			return errors.Wrap(err, "could not sync history")
		}
		ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), true, false)
		i.logger.Info("iBFT implementation init finished")
	}

	return nil
}

func (i *Controller) initialized() bool {
	return i.initHandlers.Get() && i.initSynced.Get()
}

// StartInstance - starts an ibft instance or returns error
func (i *Controller) StartInstance(opts ibft.ControllerStartInstanceOptions) (res *ibft.InstanceResult, err error) {
	instanceOpts, err := i.instanceOptionsFromStartOptions(opts)
	if err != nil {
		return nil, errors.WithMessage(err, "can't generate instance options")
	}

	if err := i.canStartNewInstance(*instanceOpts); err != nil {
		return nil, errors.WithMessage(err, "can't start new iBFT instance")
	}

	done := reportIBFTInstanceStart(i.ValidatorShare.PublicKey.SerializeToHexStr())

	res, err = i.startInstanceWithOptions(instanceOpts, opts.Value)
	defer func() {
		done()
		// report error status if the instance returned error
		if err != nil {
			ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), true, true)
			return
		}
	}()

	return res, err
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
