package controller

import (
	"sync"
	"sync/atomic"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// ErrAlreadyRunning is used to express that some process is already running, e.g. sync
var ErrAlreadyRunning = errors.New("already running")

// Controller implements Controller interface
type Controller struct {
	currentInstance instance.Instance
	logger          *zap.Logger
	ibftStorage     qbftstorage.Iibft
	network         p2pprotocol.Network
	instanceConfig  *qbft.InstanceConfig
	ValidatorShare  *keymanager.Share
	Identifier      []byte
	fork            forks.Fork
	signer          beaconprotocol.Signer

	// flags
	initHandlers atomic.Value // bool
	initSynced   atomic.Value // bool

	// locks
	currentInstanceLock sync.Locker
	syncingLock         *semaphore.Weighted

	syncRateLimit time.Duration
}

// New is the constructor of Controller
func New(
	role beaconprotocol.RoleType,
	identifier []byte,
	logger *zap.Logger,
	storage qbftstorage.Iibft,
	network p2pprotocol.Network,
	instanceConfig *qbft.InstanceConfig,
	validatorShare *keymanager.Share,
	version forksprotocol.ForkVersion,
	signer beaconprotocol.Signer,
	syncRateLimit time.Duration,
) IController {
	logger = logger.With(zap.String("role", role.String()))
	fork := forksfactory.NewFork(version)

	ret := &Controller{
		ibftStorage:    storage,
		logger:         logger,
		network:        network,
		instanceConfig: instanceConfig,
		ValidatorShare: validatorShare,
		Identifier:     identifier,
		signer:         signer,
		fork:           fork,

		// locks
		currentInstanceLock: &sync.Mutex{},
		syncingLock:         semaphore.NewWeighted(1),

		syncRateLimit: syncRateLimit,
	}

	// set flags
	ret.initHandlers.Store(false)
	ret.initSynced.Store(false)

	return ret
}

// Init sets all major processes of iBFT while blocking until completed.
// if init fails to sync
func (i *Controller) Init() error {
	if !i.isInitHandlers() {
		i.initHandlers.Store(true)
		i.logger.Info("iBFT implementation init started")
		ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		//i.processDecidedQueueMessages()
		i.processSyncQueueMessages()
		i.listenToSyncMessages()
		i.logger.Debug("managed to setup iBFT handlers")
	}

	if !i.synced() {
		// IBFT sync to make sure the operator is aligned for this validator
		if err := i.SyncIBFT(); err != nil {
			if err == ErrAlreadyRunning {
				// don't fail if init is already running
				i.logger.Debug("iBFT init is already running (syncing history)")
				return nil
			}
			i.logger.Warn("iBFT implementation init failed to sync history", zap.Error(err))
			ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), false, true)
			return errors.Wrap(err, "could not sync history")
		}
		ReportIBFTStatus(i.ValidatorShare.PublicKey.SerializeToHexStr(), true, false)
		i.logger.Info("iBFT implementation init finished")
	}

	return nil
}

// initialized return true is both isInitHandlers and synced
func (i *Controller) initialized() bool {
	return i.isInitHandlers() && i.synced()
}

// synced return true if syncer synced
func (i *Controller) synced() bool {
	return i.initSynced.Load().(bool)
}

// isInitHandlers return true if handlers init
func (i *Controller) isInitHandlers() bool {
	return i.initHandlers.Load().(bool)
}

// StartInstance - starts an ibft instance or returns error
func (i *Controller) StartInstance(opts instance.ControllerStartInstanceOptions) (res *instance.InstanceResult, err error) {
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
func (i *Controller) GetIBFTCommittee() map[keymanager.OperatorID]*keymanager.Node {
	return i.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (i *Controller) GetIdentifier() []byte {
	return i.Identifier // TODO should use mutex to lock var?
}

func (i *Controller) ProcessMsg(msg *message.SignedMessage) {
	switch msg.Message.MsgType {
	case message.ProposalMsgType:
	case message.PrepareMsgType:
	case message.CommitMsgType:
		// TODO check if instance nil?
		i.currentInstance.ProcessMsg(msg)
	case message.RoundChangeMsgType:
	case message.DecidedMsgType:
		i.ProcessDecidedMessage(msg)
	}
}
