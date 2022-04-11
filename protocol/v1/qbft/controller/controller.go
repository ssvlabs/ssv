package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
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
	ctx         context.Context

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

	q msgqueue.MsgQueue

	syncDecided SyncDecided
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

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.SignedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	if err != nil {
		// TODO: we should probably stop here, TBD
		logger.Warn("could not setup msg queue properly", zap.Error(err))
	}
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

		q: q,
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

	if !c.synced() {
		// IBFT sync to make sure the operator is aligned for this validator
		if err := c.syncDecided(i.ctx, &SyncContext{
			Store:      i.ibftStorage,
			Syncer:     i.network,
			Validate:   i.fork.ValidateDecidedMsg(i.ValidatorShare),
			Identifier: i.GetIdentifier(),
		}); err != nil {
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

// ProcessMsg takes an incoming message, and adds it to the message queue or handle it on read mode
func (i *Controller) ProcessMsg(msg *message.SSVMessage) (bool, []byte, error) {
	if i.readMode {
		err := i.messageHandler(msg)
		return false, nil, err
	}
	i.q.Add(msg)
	return false, nil, nil
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
