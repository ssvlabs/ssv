package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

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
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
)

// ErrAlreadyRunning is used to express that some process is already running, e.g. sync
var ErrAlreadyRunning = errors.New("already running")

// Options is a set of options for the controller
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

// set of states for the controller
const (
	NotStarted uint32 = iota
	InitiatedHandlers
	WaitingForPeers
	FoundPeers
	Ready
	Forking
)

// Controller implements Controller interface
type Controller struct {
	ctx context.Context

	currentInstanceLock  sync.Locker
	currentInstanceRLock sync.Locker
	currentInstance      instance.Instancer
	logger               *zap.Logger
	instanceStorage      qbftstorage.InstanceStore
	changeRoundStorage   qbftstorage.ChangeRoundStore
	network              p2pprotocol.Network
	instanceConfig       *qbft.InstanceConfig
	ValidatorShare       *beaconprotocol.Share
	Identifier           message.Identifier
	forkLock             sync.Locker
	fork                 forks.Fork
	beacon               beaconprotocol.Beacon
	signer               beaconprotocol.Signer

	// signature
	signatureState SignatureState

	// flags
	state uint32

	syncRateLimit time.Duration

	// flags
	readMode bool
	fullNode bool

	q msgqueue.MsgQueue

	decidedFactory  *factory.Factory
	decidedStrategy strategy.Decided
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
	currentInstanceLock := &sync.RWMutex{}
	ctrl := &Controller{
		ctx:                opts.Context,
		instanceStorage:    opts.Storage,
		changeRoundStorage: opts.Storage,
		logger:             logger,
		network:            opts.Network,
		instanceConfig:     opts.InstanceConfig,
		ValidatorShare:     opts.ValidatorShare,
		Identifier:         opts.Identifier,
		fork:               fork,
		beacon:             opts.Beacon,
		signer:             opts.Signer,
		signatureState:     SignatureState{SignatureCollectionTimeout: opts.SigTimeout},

		syncRateLimit: opts.SyncRateLimit,

		readMode: opts.ReadMode,
		fullNode: opts.FullNode,

		q: q,

		currentInstanceLock:  currentInstanceLock,
		currentInstanceRLock: currentInstanceLock.RLocker(),
		forkLock:             &sync.Mutex{},
	}

	ctrl.decidedFactory = factory.NewDecidedFactory(logger, ctrl.isFullNode(), opts.Storage, opts.Network)
	ctrl.decidedStrategy = ctrl.decidedFactory.GetStrategy()

	// set flags
	ctrl.state = NotStarted

	return ctrl
}

func (c *Controller) getCurrentInstance() instance.Instancer {
	c.currentInstanceRLock.Lock()
	defer c.currentInstanceRLock.Unlock()

	return c.currentInstance
}

func (c *Controller) setCurrentInstance(instance instance.Instancer) {
	c.currentInstanceLock.Lock()
	defer c.currentInstanceLock.Unlock()

	c.currentInstance = instance
}

// OnFork called upon fork, it will make sure all decided messages were processed
// before clearing the entire msg queue.
// it also recreates the fork instance and decided strategy with the new fork version
func (c *Controller) OnFork(forkVersion forksprotocol.ForkVersion) error {
	atomic.StoreUint32(&c.state, Forking)
	defer atomic.StoreUint32(&c.state, Ready)

	c.processAllDecided(c.messageHandler)
	cleared := c.q.Clean(msgqueue.AllIndicesCleaner)
	c.logger.Debug("FORKING qbft controller", zap.Int64("clearedMessages", cleared))

	// get new QBFT controller fork and update decidedStrategy
	c.forkLock.Lock()
	defer c.forkLock.Unlock()
	c.fork = forksfactory.NewFork(forkVersion)
	c.decidedStrategy = c.decidedFactory.GetStrategy()
	return nil
}

func (c *Controller) syncDecided() error {
	c.logger.Debug("syncing decided", zap.String("identifier", c.Identifier.String()))
	c.forkLock.Lock()
	fork := c.fork
	decidedStrategy := c.decidedStrategy
	c.forkLock.Unlock()
	return decidedStrategy.Sync(c.ctx, c.Identifier, fork.ValidateDecidedMsg(c.ValidatorShare))
}

// Init sets all major processes of iBFT while blocking until completed.
// if init fails to sync
func (c *Controller) Init() error {
	// checks if notStarted. if so, preform init handlers and set state to new state
	if atomic.CompareAndSwapUint32(&c.state, NotStarted, InitiatedHandlers) {
		c.logger.Info("start qbft ctrl handler init")
		go c.startQueueConsumer(c.messageHandler)
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		//c.logger.Debug("managed to setup iBFT handlers")
	}

	// if already waiting for peers no need to redundant waiting
	if atomic.LoadUint32(&c.state) == WaitingForPeers {
		return ErrAlreadyRunning
	}

	// only if finished with handlers, start waiting for peers and syncing
	if atomic.CompareAndSwapUint32(&c.state, InitiatedHandlers, WaitingForPeers) {
		// warmup to avoid network errors
		time.Sleep(500 * time.Millisecond)
		minPeers := 1
		c.logger.Debug("waiting for min peers...", zap.Int("min peers", minPeers))
		if err := p2pprotocol.WaitForMinPeers(c.ctx, c.logger, c.network, c.ValidatorShare.PublicKey.Serialize(), minPeers, time.Millisecond*500); err != nil {
			return err
		}
		c.logger.Debug("found enough peers")

		atomic.StoreUint32(&c.state, FoundPeers)

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

		atomic.StoreUint32(&c.state, Ready)

		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), true, false)
		c.logger.Info("iBFT implementation init finished")
	}

	return nil
}

// initialized return true is done the init process and not in forking state
func (c *Controller) initialized() (bool, error) {
	state := atomic.LoadUint32(&c.state)
	switch state {
	case Ready:
		return true, nil
	case Forking:
		return false, errors.New("forking in progress")
	default:
		return false, errors.New("iBFT hasn't initialized yet")
	}
}

// StartInstance - starts an ibft instance or returns error
func (c *Controller) StartInstance(opts instance.ControllerStartInstanceOptions) (res *instance.Result, err error) {
	instanceOpts, err := c.instanceOptionsFromStartOptions(opts)
	if err != nil {
		return nil, errors.WithMessage(err, "can't generate instance options")
	}

	if err := c.canStartNewInstance(*instanceOpts); err != nil {
		return nil, errors.WithMessage(err, "can't start new iBFT instance")
	}

	done := reportIBFTInstanceStart(c.ValidatorShare.PublicKey.SerializeToHexStr())

	c.signatureState.setHeight(opts.SeqNumber)                       // update sig state once height determent
	instanceOpts.ChangeRoundStore = c.changeRoundStorage             // in order to set the last change round msg
	instanceOpts.ChangeRoundStore.CleanLastChangeRound(c.Identifier) // clean previews last change round msg's (TODO place in instance?)
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
	cInstance := c.getCurrentInstance()
	if cInstance != nil {
		currentState := cInstance.State()
		if currentState != nil {
			fields = append(fields, zap.String("stage", qbft.RoundStateName[currentState.Stage.Load()]), zap.Uint32("height", uint32(currentState.GetHeight())), zap.Uint32("round", uint32(currentState.GetRound())))
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
