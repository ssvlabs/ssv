package controller

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/fullnode"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/node"
	"go.uber.org/atomic"
	"sync"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// ErrAlreadyRunning is used to express that some process is already running, e.g. sync
var ErrAlreadyRunning = errors.New("already running")

type Options struct {
	Context        context.Context
	Role           message.RoleType
	Identifier     message.Identifier
	Logger         *zap.Logger
	Storage        qbftstorage.QBFTStore
	Network        p2pprotocol.Network
	InstanceConfig *qbft.InstanceConfig
	ValidatorShare *beaconprotocol.Share
	Version        forksprotocol.ForkVersion
	Beacon         beaconprotocol.Beacon
	Signer         beaconprotocol.Signer
	SyncRateLimit  time.Duration
	SigTimeout     time.Duration
	ReadMode       bool
	FullNode       bool
}

// Controller implements Controller interface
type Controller struct {
	ctx context.Context

	currentInstance instance.Instancer
	logger          *zap.Logger
	ibftStorage     qbftstorage.QBFTStore
	network         p2pprotocol.Network
	instanceConfig  *qbft.InstanceConfig
	ValidatorShare  *beaconprotocol.Share
	Identifier      message.Identifier
	fork            forks.Fork
	beacon          beaconprotocol.Beacon
	signer          beaconprotocol.Signer

	// signature
	signatureState SignatureState

	// flags
	initHandlers atomic.Bool // bool
	initSynced   atomic.Bool // bool

	// locks
	currentInstanceLock sync.Locker
	syncingLock         *semaphore.Weighted

	syncRateLimit time.Duration

	// flags
	readMode bool
	fullNode bool

	q msgqueue.MsgQueue

	strategy strategy.Decided
}

// New is the constructor of Controller
func New(opts Options) IController {
	logger := opts.Logger.With(zap.String("role", opts.Role.String()), zap.Bool("read mode", opts.ReadMode))
	fork := forksfactory.NewFork(opts.Version)

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.DefaultMsgIndexer(), msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	if err != nil {
		// TODO: we should probably stop here, TBD
		logger.Warn("could not setup msg queue properly", zap.Error(err))
	}
	ctrl := &Controller{
		ctx:            opts.Context,
		ibftStorage:    opts.Storage,
		logger:         logger,
		network:        opts.Network,
		instanceConfig: opts.InstanceConfig,
		ValidatorShare: opts.ValidatorShare,
		Identifier:     opts.Identifier,
		fork:           fork,
		beacon:         opts.Beacon,
		signer:         opts.Signer,
		signatureState: SignatureState{SignatureCollectionTimeout: opts.SigTimeout},

		// locks
		currentInstanceLock: &sync.Mutex{},
		syncingLock:         semaphore.NewWeighted(1),

		syncRateLimit: opts.SyncRateLimit,

		readMode: opts.ReadMode,
		fullNode: opts.FullNode,

		q: q,
	}

	// create strategy
	if ctrl.isFullNode() {
		ctrl.strategy = fullnode.NewFullNodeStrategy(logger, opts.Storage, opts.Network)
	} else {
		ctrl.strategy = node.NewRegularNodeStrategy(logger, opts.Storage, opts.Network)
	}

	// set flags
	ctrl.initHandlers.Store(false)
	ctrl.initSynced.Store(false)

	return ctrl
}

// OnFork called when fork occur.
func (c *Controller) OnFork(forkVersion forksprotocol.ForkVersion) error {
	// get new QBFT controller fork
	c.fork = forksfactory.NewFork(forkVersion)
	// update strategy
	if c.isFullNode() {
		c.strategy = fullnode.NewFullNodeStrategy(c.logger, c.ibftStorage, c.network)
	} else {
		c.strategy = node.NewRegularNodeStrategy(c.logger, c.ibftStorage, c.network)
	}
	return nil
}

func (c *Controller) syncDecided() error {
	c.logger.Debug("syncing decided", zap.String("identifier", c.Identifier.String()))

	highest, err := c.strategy.Sync(c.ctx, c.Identifier, c.fork.ValidateDecidedMsg(c.ValidatorShare))
	if err == nil && highest != nil {
		err = c.ibftStorage.SaveLastDecided(highest)
	}

	return err
}

// Init sets all major processes of iBFT while blocking until completed.
// if init fails to sync
func (c *Controller) Init() error {
	if !c.initHandlers.Load() {
		c.initHandlers.Store(true)
		c.logger.Info("iBFT implementation init started")
		go c.startQueueConsumer(c.messageHandler)
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		//c.logger.Debug("managed to setup iBFT handlers")
	}

	if !c.initSynced.Load() {
		// warmup to avoid network errors
		time.Sleep(500 * time.Millisecond)
		minPeers := 1
		c.logger.Debug("waiting for min peers...", zap.Int("min peers", minPeers))
		if err := p2pprotocol.WaitForMinPeers(c.ctx, c.logger, c.network, c.ValidatorShare.PublicKey.Serialize(), minPeers, time.Millisecond*500); err != nil {
			return err
		}
		c.logger.Debug("found enough peers")
		// IBFT sync to make sure the operator is aligned for this validator
		if err := c.syncDecided(); err != nil {
			if err == ErrAlreadyRunning {
				// don't fail if init is already running
				c.logger.Debug("iBFT init is already running (syncing history)")
				return nil
			}
			c.logger.Warn("iBFT implementation init failed to sync history", zap.Error(err))
			ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, true)
			return errors.Wrap(err, "could not sync history")
		}
		c.initSynced.Store(true)
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), true, false)
		c.logger.Info("iBFT implementation init finished")
	}

	return nil
}

// initialized return true is both isInitHandlers and synced
func (c *Controller) initialized() bool {
	return c.initHandlers.Load() && c.initSynced.Load()
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

	c.signatureState.height = opts.SeqNumber // update sig state once height determent
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
func (c *Controller) GetIBFTCommittee() map[message.OperatorID]*beaconprotocol.Node {
	return c.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (c *Controller) GetIdentifier() []byte {
	return c.Identifier // TODO should use mutex to lock var?
}

// ProcessMsg takes an incoming message, and adds it to the message queue or handle it on read mode
func (c *Controller) ProcessMsg(msg *message.SSVMessage) error {
	if c.readMode {
		return c.messageHandler(msg)
	}
	var fields []zap.Field
	cInstance := c.currentInstance
	if cInstance != nil {
		currentState := cInstance.State()
		if currentState != nil {
			fields = append(fields, zap.String("stage", currentState.Stage.String()), zap.Uint32("height", uint32(currentState.GetHeight())), zap.Uint32("round", uint32(currentState.GetRound())))
		}
	}
	fields = append(fields, zap.Any("msg", msg))
	c.logger.Debug("got message, add to queue", fields...)
	c.q.Add(msg)
	return nil
}

// messageHandler process message from queue,
func (c *Controller) messageHandler(msg *message.SSVMessage) error {
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
	case message.SSVDecidedMsgType:
		signedMsg := &message.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from SSVMessage")
		}
		return c.processDecidedMessage(signedMsg)
	case message.SSVSyncMsgType:
		panic("need to implement!")
	}
	return nil
}

func (c *Controller) isFullNode() bool {
	isPostFork := c.fork.VersionName() != forksprotocol.V0ForkVersion.String()
	if !isPostFork { // by default when pre fork, full sync is true
		return true
	}
	// otherwise, checking flag
	if c.fullNode {
		return true
	}
	return false
}
