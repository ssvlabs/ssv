package instance

import (
	"context"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/instance/eventqueue"
	msgcontinmem "github.com/bloxapp/ssv/ibft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/instance/roundtimer"
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/utils/format"

	"github.com/pkg/errors"
	"go.uber.org/atomic"
	"go.uber.org/zap"
)

// Options defines option attributes for the Instance
type Options struct {
	Logger         *zap.Logger
	ValidatorShare *message.Share
	// Me             *proto.Node
	Network        network.P2PNetwork
	Queue          *msgqueue.MessageQueue
	ValueCheck     valcheck.ValueCheck
	LeaderSelector leader.Selector
	Config         *proto.InstanceConfig
	Lambda         []byte
	SeqNumber      uint64
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
	// Fork sets the current fork to apply on instance
	Fork   forks.Fork
	Signer beaconprotocol.Signer
}

// Instance defines the instance attributes
type Instance struct {
	ValidatorShare *message.Share
	state          *qbft.State
	network        network.P2PNetwork
	ValueCheck     valcheck.ValueCheck
	LeaderSelector leader.Selector
	Config         *proto.InstanceConfig
	roundTimer     *roundtimer.RoundTimer
	Logger         *zap.Logger
	fork           forks.Fork
	signer         beaconprotocol.Signer

	// messages
	MsgQueue            *msgqueue.MessageQueue
	PrePrepareMessages  msgcont.MessageContainer
	PrepareMessages     msgcont.MessageContainer
	CommitMessages      msgcont.MessageContainer
	ChangeRoundMessages msgcont.MessageContainer
	lastChangeRoundMsg  *message.SignedMessage // lastChangeRoundMsg stores the latest change round msg broadcasted, used for fast instance catchup
	decidedMsg          *message.SignedMessage

	// event loop
	eventQueue eventqueue.EventQueue

	// channels
	stageChangedChan chan qbft.RoundState

	// flags
	stopped     bool
	initialized bool

	// locks
	runInitOnce                  sync.Once
	runStopOnce                  sync.Once
	processChangeRoundQuorumOnce sync.Once
	processPrepareQuorumOnce     sync.Once
	processCommitQuorumOnce      sync.Once
	stopLock                     sync.Mutex
	lastChangeRoundMsgLock       sync.RWMutex
	stageChanCloseChan           sync.Mutex
}

// NewInstanceWithState used for testing, not PROD!
func NewInstanceWithState(state *qbft.State) Instancer {
	return &Instance{
		state: state,
	}
}

// NewInstance is the constructor of Instance
func NewInstance(opts *Options) Instancer {
	pk, role := format.IdentifierUnformat(string(opts.Lambda))
	metricsIBFTStage.WithLabelValues(role, pk).Set(float64(proto.RoundState_NotStarted))
	logger := opts.Logger.With(zap.Uint64("seq_num", opts.SeqNumber))

	ret := &Instance{
		ValidatorShare: opts.ValidatorShare,
		state:          generateState(opts),
		network:        opts.Network,
		ValueCheck:     opts.ValueCheck,
		LeaderSelector: opts.LeaderSelector,
		Config:         opts.Config,
		Logger:         logger,
		signer:         opts.Signer,

		MsgQueue:            opts.Queue,
		PrePrepareMessages:  msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		PrepareMessages:     msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		CommitMessages:      msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		ChangeRoundMessages: msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),

		roundTimer: roundtimer.New(context.Background(), logger.With(zap.String("who", "RoundTimer"))),

		eventQueue: eventqueue.New(),

		// locks
		runInitOnce:                  sync.Once{},
		runStopOnce:                  sync.Once{},
		processChangeRoundQuorumOnce: sync.Once{},
		processPrepareQuorumOnce:     sync.Once{},
		processCommitQuorumOnce:      sync.Once{},
		stopLock:                     sync.Mutex{},
		lastChangeRoundMsgLock:       sync.RWMutex{},
		stageChanCloseChan:           sync.Mutex{},
	}

	ret.setFork(opts.Fork)

	return ret
}

// Init must be called before start can be
func (i *Instance) Init() {
	i.runInitOnce.Do(func() {
		go i.StartMessagePipeline()
		go i.startRoundTimerLoop()
		go i.StartMainEventLoop()
		i.initialized = true
		i.Logger.Debug("iBFT instance init finished")
	})
}

// State returns instance state
func (i *Instance) State() *qbft.State {
	return i.state
}

// Start implements the Algorithm 1 IBFTController pseudocode for process pi: constants, state variables, and ancillary procedures
// procedure Start(λ, value)
// 	λi ← λ
// 	ri ← 1
// 	pri ← ⊥
// 	pvi ← ⊥
// 	inputV aluei ← value
// 	if leader(hi, ri) = pi then
// 		broadcast ⟨PRE-PREPARE, λi, ri, inputV aluei⟩ message
// 		set timer to running and expire after t(ri)
func (i *Instance) Start(inputValue []byte) error {
	if !i.initialized {
		return errors.New("instance not initialized")
	}
	if i.State().GetIdentifier() == nil {
		return errors.New("invalid Lambda")
	}
	if inputValue == nil {
		return errors.New("input value is nil")
	}

	i.Logger.Info("Node is starting iBFT instance", zap.String("Lambda", i.State().Identifier.Load()))
	i.State().InputValue.Store(inputValue)
	i.State().Round.Store(message.Round(1)) // start from 1
	pk, role := format.IdentifierUnformat(i.State().Identifier.Load())
	metricsIBFTRound.WithLabelValues(role, pk).Set(1)

	if i.IsLeader() {
		go func() {
			i.Logger.Info("Node is leader for round 1")
			i.ProcessStageChange(qbft.RoundState_PrePrepare)

			// LeaderPreprepareDelaySeconds waits to let other nodes complete their instance start or round change.
			// Waiting will allow a more stable msg receiving for all parties.
			time.Sleep(time.Duration(i.Config.LeaderPreprepareDelaySeconds))

			msg := i.generatePrePrepareMessage(i.State().GetInputValue())
			//
			if err := i.SignAndBroadcast(msg); err != nil {
				i.Logger.Fatal("could not broadcast pre-prepare", zap.Error(err))
			}
		}()
	}
	i.resetRoundTimer()
	return nil
}

// ForceDecide will attempt to decide the instance with provided decided signed msg.
func (i *Instance) ForceDecide(msg *message.SignedMessage) {
	i.eventQueue.Add(eventqueue.NewEvent(func() {
		i.Logger.Info("trying to force instance decision.")
		if err := i.DecidedMsgPipeline().Run(msg); err != nil {
			i.Logger.Error("force decided pipeline error", zap.Error(err))
		}
	}))
}

// Stop will trigger a stopped for the entire instance
func (i *Instance) Stop() {
	// stop can be run just once
	i.runStopOnce.Do(func() {
		if added := i.eventQueue.Add(eventqueue.NewEvent(i.stop)); !added {
			i.Logger.Debug("could not add 'stop' to event queue")
		}
	})
}

// stop stops the instance
func (i *Instance) stop() {
	i.Logger.Info("stopping iBFT instance...")
	i.stopLock.Lock()
	defer i.stopLock.Unlock()
	i.Logger.Debug("STOPPING IBFTController -> pass stopLock")
	i.stopped = true
	i.roundTimer.Kill()
	i.Logger.Debug("STOPPING IBFTController -> stopped round timer")
	i.ProcessStageChange(qbft.RoundState_Stopped)
	i.Logger.Debug("STOPPING IBFTController -> set stage to stop")
	i.eventQueue.ClearAndStop()
	i.Logger.Debug("STOPPING IBFTController -> cleared event queue")

	// stop stage chan
	i.Logger.Debug("STOPPING IBFTController -> passed stageLock")
	if i.stageChangedChan != nil {
		i.stageChanCloseChan.Lock() // in order to prevent from sending to a close chan
		close(i.stageChangedChan)
		i.Logger.Debug("STOPPING IBFTController -> closed stageChangedChan")
		i.stageChangedChan = nil
		i.stageChanCloseChan.Unlock()
	}

	i.Logger.Info("stopped iBFT instance")
}

// Stopped is stopping queue work
func (i *Instance) Stopped() bool {
	i.stopLock.Lock()
	defer i.stopLock.Unlock()

	return i.stopped
}

func (i *Instance) ProcessMsg(msg *message.SignedMessage) error {
	var pp validation.SignedMessagePipeline

	switch msg.Message.MsgType {
	case message.ProposalMsgType:
		pp = i.PrePrepareMsgPipeline()
	case message.PrepareMsgType:
		pp = i.PrepareMsgPipeline()
	case message.CommitMsgType:
		pp = i.CommitMsgPipeline()
	case message.RoundChangeMsgType:
		pp = i.ChangeRoundMsgPipeline()
	default:
		i.Logger.Warn("undefined message type", zap.Any("msg", msg))
		return errors.Errorf("undefined message type")
	}
	return pp.Run(msg)
}

// BumpRound is used to set bump round by 1
func (i *Instance) BumpRound() {
	i.bumpToRound(uint64(i.State().GetRound() + 1))
}

func (i *Instance) bumpToRound(round uint64) {
	i.processChangeRoundQuorumOnce = sync.Once{}
	i.processPrepareQuorumOnce = sync.Once{}
	newRound := round
	i.State().SetRound(round)
	pk, role := format.IdentifierUnformat(string(i.State().GetIdentifier()))
	metricsIBFTRound.WithLabelValues(role, pk).Set(float64(newRound))
}

// ProcessStageChange set the state's round state and pushed the new state into the state channel
func (i *Instance) ProcessStageChange(stage qbft.RoundState) {
	pk, role := format.IdentifierUnformat(string(i.State().GetIdentifier()))
	metricsIBFTStage.WithLabelValues(role, pk).Set(float64(stage))

	i.State().Stage.Store(int32(stage))

	// blocking send to channel
	i.stageChanCloseChan.Lock()
	defer i.stageChanCloseChan.Unlock()
	if i.stageChangedChan != nil {
		i.stageChangedChan <- stage
	}
}

// GetStageChan returns a RoundState channel added to the stateChangesChans array
func (i *Instance) GetStageChan() chan qbft.RoundState {
	if i.stageChangedChan == nil {
		i.stageChangedChan = make(chan qbft.RoundState)
	}
	return i.stageChangedChan
}

// SignAndBroadcast checks and adds the signed message to the appropriate round state type
func (i *Instance) SignAndBroadcast(msg *message.ConsensusMessage) error {
	pk, err := i.ValidatorShare.OperatorPubKey()
	if err != nil {
		return errors.Wrap(err, "could not find operator pk for signing msg")
	}

	sigByts, err := i.signer.SignIBFTMessage(msg, pk.Serialize())
	if err != nil {
		return err
	}

	signedMessage := &message.SignedMessage{
		Message:   msg,
		Signature: sigByts,
		Signers:   []keymanager.OperatorID{i.ValidatorShare.NodeID},
	}

	// used for instance fast change round catchup
	if msg.MsgType == message.RoundChangeMsgType {
		i.setLastChangeRoundMsg(signedMessage)
	}

	if i.network != nil {
		return i.network.Broadcast(i.ValidatorShare.PublicKey.Serialize(), signedMessage)
	}
	return errors.New("no networking, could not broadcast msg")
}

func (i *Instance) setLastChangeRoundMsg(msg *message.SignedMessage) {
	i.lastChangeRoundMsgLock.Lock()
	defer i.lastChangeRoundMsgLock.Unlock()
	i.lastChangeRoundMsg = msg
}

// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
func (i *Instance) GetLastChangeRoundMsg() *message.SignedMessage {
	i.lastChangeRoundMsgLock.RLock()
	defer i.lastChangeRoundMsgLock.RUnlock()
	return i.lastChangeRoundMsg
}

// CommittedAggregatedMsg returns a signed message for the state's committed value with the max known signatures
func (i *Instance) CommittedAggregatedMsg() (*message.SignedMessage, error) {
	if i.State() == nil {
		return nil, errors.New("missing instance state")
	}
	if i.decidedMsg != nil {
		return i.decidedMsg, nil
	}
	return nil, errors.New("missing decided message")
}

func (i *Instance) setFork(fork forks.Fork) {
	if fork == nil {
		return
	}
	i.fork = fork
	i.fork.Apply(i)
}

func generateState(opts *Options) *qbft.State {
	var stage atomic.Int32
	var identifier atomic.Value
	var height, round atomic.Value
	height.Store(message.Height(opts.SeqNumber))
	round.Store(message.Round(1))
	stage.Store(int32(qbft.RoundState_NotStarted))
	identifier.Store(opts.Lambda)

	return &qbft.State{
		Stage:         stage,
		Identifier:    identifier,
		Height:        height,
		InputValue:    atomic.Value{},
		Round:         round,
		PreparedRound: atomic.Uint64{},
		PreparedValue: atomic.String{},
	}
}
