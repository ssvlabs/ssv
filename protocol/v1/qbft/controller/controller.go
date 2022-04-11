package controller

import (
	"sync"
	"sync/atomic"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
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
	currentInstance instance.Instancer
	logger          *zap.Logger
	ibftStorage     qbftstorage.QBFTStore
	network         p2pprotocol.Network
	instanceConfig  *qbft.InstanceConfig
	ValidatorShare  *message.Share
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

	// flags
	readMode bool
}

// New is the constructor of Controller
func New(
	role beaconprotocol.RoleType,
	identifier []byte,
	logger *zap.Logger,
	storage qbftstorage.QBFTStore,
	network p2pprotocol.Network,
	instanceConfig *qbft.InstanceConfig,
	validatorShare *message.Share,
	version forksprotocol.ForkVersion,
	signer beaconprotocol.Signer,
	syncRateLimit time.Duration,
	readMode bool,
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

		readMode: readMode,
	}

	// set flags
	ret.initHandlers.Store(false)
	ret.initSynced.Store(false)

	return ret
}

// Init sets all major processes of iBFT while blocking until completed.
// if init fails to sync
func (c *Controller) Init() error {
	if !c.isInitHandlers() {
		c.initHandlers.Store(true)
		c.logger.Info("iBFT implementation init started")
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		//c.processDecidedQueueMessages()
		c.processSyncQueueMessages()
		c.listenToSyncMessages()
		c.logger.Debug("managed to setup iBFT handlers")
	}

	if !c.synced() {
		// IBFT sync to make sure the operator is aligned for this validator
		if err := c.SyncIBFT(); err != nil {
			if err == ErrAlreadyRunning {
				// don't fail if init is already running
				c.logger.Debug("iBFT init is already running (syncing history)")
				return nil
			}
			c.logger.Warn("iBFT implementation init failed to sync history", zap.Error(err))
			ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, true)
			return errors.Wrap(err, "could not sync history")
		}
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), true, false)
		c.logger.Info("iBFT implementation init finished")
	}

	return nil
}

// initialized return true is both isInitHandlers and synced
func (c *Controller) initialized() bool {
	return c.isInitHandlers() && c.synced()
}

// synced return true if syncer synced
func (c *Controller) synced() bool {
	return c.initSynced.Load().(bool)
}

// isInitHandlers return true if handlers init
func (c *Controller) isInitHandlers() bool {
	return c.initHandlers.Load().(bool)
}

// StartInstance - starts an ibft instance or returns error
func (c *Controller) StartInstance(opts instance.ControllerStartInstanceOptions) (res *instance.InstanceResult, err error) {
	instanceOpts, err := c.instanceOptionsFromStartOptions(opts)
	if err != nil {
		return nil, errors.WithMessage(err, "can't generate instance options")
	}

	if err := c.canStartNewInstance(*instanceOpts); err != nil {
		return nil, errors.WithMessage(err, "can't start new iBFT instance")
	}

	done := reportIBFTInstanceStart(c.ValidatorShare.PublicKey.SerializeToHexStr())

	res, err = c.startInstanceWithOptions(instanceOpts, opts.Value)
	defer func() {
		done()
		// report error status if the instance returned error
		if err != nil {
			ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), true, true)
			return
		}
	}()

	return res, err
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (c *Controller) GetIBFTCommittee() map[message.OperatorID]*message.Node {
	return c.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (c *Controller) GetIdentifier() []byte {
	return c.Identifier // TODO should use mutex to lock var?
}

func (c *Controller) ProcessMsg(msg *message.SSVMessage) error {
	if c.readMode {
		if err := c.messageHandler(msg); err != nil {
			return err
		}
	} else {
		// TODO add to queue
	}
	return nil
}

// messageHandler process message from queue,
func (c *Controller) messageHandler(msg *message.SSVMessage) error {
	// validation
	if err := c.validateMessage(msg); err != nil {
		// TODO need to return error?
		c.logger.Error("message validation failed", zap.Error(err))
		return nil
	}

	switch msg.GetType() {
	case message.SSVConsensusMsgType:
		signedMsg := &message.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from SSVMessage")
		}
		return c.processConsensusMsg(signedMsg)

	case message.SSVPostConsensusMsgType:
		signedMsg := &message.SignedPostConsensusMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		return c.processPostConsensusSig(signedMsg)
	case message.SSVSyncMsgType:
		panic("need to implement!")
	}
	return nil
}
