package controller

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/sync/history"
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
	Identifier     []byte
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
	Identifier      []byte
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

	q msgqueue.MsgQueue

	//	TODO - test var needs to be removed
	incomeMsgs    atomic.Int32
	processedMsgs atomic.Int32
}

// New is the constructor of Controller
func New(opts Options) IController {
	logger := opts.Logger.With(zap.String("role", opts.Role.String()), zap.Bool("read mode", opts.ReadMode))
	fork := forksfactory.NewFork(opts.Version)

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.DefaultMsgIndexer(), msgqueue.SignedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	if err != nil {
		// TODO: we should probably stop here, TBD
		logger.Warn("could not setup msg queue properly", zap.Error(err))
	}
	ret := &Controller{
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

		q:             q,
		incomeMsgs:    *atomic.NewInt32(0),
		processedMsgs: *atomic.NewInt32(0),
	}

	// set flags
	ret.initHandlers.Store(false)
	ret.initSynced.Store(false)
	return ret
}

// OnFork called when fork occur.
func (c *Controller) OnFork(forkVersion forksprotocol.ForkVersion) error {
	c.fork = forksfactory.NewFork(forkVersion)
	// TODO needs to lock fork?
	// TODO need to stop instance?
	return nil
}

func (c *Controller) syncDecided() error {
	h := history.New(c.logger, c.network)

	handler := func(msg *message.SignedMessage) error {
		err := c.fork.ValidateDecidedMsg(c.ValidatorShare).Run(msg)
		if err != nil {
			return errors.Wrap(err, "invalid msg")
		}
		// TODO: exit if already exist
		return c.ibftStorage.SaveDecided(msg)
	}

	c.logger.Debug("syncing heights decided")
	highest, err := h.SyncDecided(c.ctx, c.Identifier, func(i message.Identifier) (*message.SignedMessage, error) {
		return c.ibftStorage.GetLastDecided(i)
	}, handler)
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
		go c.startQueueConsumer()
		ReportIBFTStatus(c.ValidatorShare.PublicKey.SerializeToHexStr(), false, false)
		//c.logger.Debug("managed to setup iBFT handlers")
	}

	if !c.initSynced.Load() {
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
	c.incomeMsgs.Inc()
	var fields []zap.Field
	if c.currentInstance != nil && c.currentInstance.State() != nil {
		state := c.currentInstance.State()
		fields = append(fields, zap.String("stage", state.Stage.String()), zap.Uint32("height", uint32(state.GetHeight())), zap.Uint32("round", uint32(state.GetRound())))
	}
	fields = append(fields, zap.Int32("incoming messages count", c.incomeMsgs.Load()), zap.String("type", msg.MsgType.String()))
	c.logger.Debug("got message, add to queue", fields...)
	c.q.Add(msg)
	return nil
}

// messageHandler process message from queue,
func (c *Controller) messageHandler(msg *message.SSVMessage) error {
	c.processedMsgs.Inc()
	c.logger.Debug("message handling", zap.Int32("processed messages count", c.processedMsgs.Load()))
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
