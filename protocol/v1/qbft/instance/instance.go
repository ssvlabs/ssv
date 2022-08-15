package instance

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	msgcontinmem "github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/roundtimer"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

// Options defines option attributes for the Instance
type Options struct {
	Logger         *zap.Logger
	ValidatorShare *beaconprotocol.Share
	Network        protcolp2p.Network
	LeaderSelector leader.Selector
	Config         *qbft.InstanceConfig
	Identifier     []byte
	Height         specqbft.Height
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
	// Fork sets the current fork to apply on instance
	Fork             forks.Fork
	SSVSigner        spectypes.SSVSigner
	ChangeRoundStore qbftstorage.ChangeRoundStore
}

// Instance defines the instance attributes
type Instance struct {
	ValidatorShare *beaconprotocol.Share
	state          *qbft.State
	network        protcolp2p.Network
	LeaderSelector leader.Selector
	Config         *qbft.InstanceConfig
	roundTimer     *roundtimer.RoundTimer
	Logger         *zap.Logger
	fork           forks.Fork
	ssvSigner      spectypes.SSVSigner

	// messages
	containersMap map[specqbft.MessageType]msgcont.MessageContainer
	decidedMsg    *specqbft.SignedMessage

	// channels
	stageChangedChan chan qbft.RoundState

	// flags
	stopped     atomic.Bool
	initialized bool

	// locks
	runInitOnce                  *sync.Once
	runStopOnce                  *sync.Once
	processChangeRoundQuorumOnce *sync.Once
	processPrepareQuorumOnce     *sync.Once
	processCommitQuorumOnce      *sync.Once
	lastChangeRoundMsgLock       sync.RWMutex
	stageChanCloseChan           sync.Mutex

	changeRoundStore qbftstorage.ChangeRoundStore
	ctx              context.Context
	cancelCtx        context.CancelFunc
}

// NewInstanceWithState used for testing, not PROD!
func NewInstanceWithState(state *qbft.State) Instancer {
	return &Instance{
		state: state,
	}
}

// NewInstance is the constructor of Instance
func NewInstance(opts *Options) Instancer {
	messageID := message.ToMessageID(opts.Identifier)
	metricsIBFTStage.WithLabelValues(messageID.GetRoleType().String(), hex.EncodeToString(messageID.GetPubKey())).Set(float64(qbft.RoundStateNotStarted))
	logger := opts.Logger.With(zap.Uint64("seq_num", uint64(opts.Height)))
	ctx, cancelCtx := context.WithCancel(context.Background())

	ret := &Instance{
		ctx:            ctx,
		cancelCtx:      cancelCtx,
		ValidatorShare: opts.ValidatorShare,
		state:          generateState(opts),
		network:        opts.Network,
		LeaderSelector: opts.LeaderSelector,
		Config:         opts.Config,
		Logger:         logger,
		ssvSigner:      opts.SSVSigner,

		roundTimer: roundtimer.New(ctx, logger.With(zap.String("who", "RoundTimer"))),

		// locks
		runInitOnce:                  &sync.Once{},
		runStopOnce:                  &sync.Once{},
		processChangeRoundQuorumOnce: &sync.Once{},
		processPrepareQuorumOnce:     &sync.Once{},
		processCommitQuorumOnce:      &sync.Once{},
		lastChangeRoundMsgLock:       sync.RWMutex{},
		stageChanCloseChan:           sync.Mutex{},

		changeRoundStore: opts.ChangeRoundStore,

		stopped: *atomic.NewBool(false),
	}

	ret.containersMap = map[specqbft.MessageType]msgcont.MessageContainer{
		specqbft.ProposalMsgType:    msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		specqbft.PrepareMsgType:     msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		specqbft.CommitMsgType:      msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
		specqbft.RoundChangeMsgType: msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize()), uint64(opts.ValidatorShare.PartialThresholdSize())),
	}

	ret.setFork(opts.Fork)

	return ret
}

// Init must be called before start can be
func (i *Instance) Init() {
	i.runInitOnce.Do(func() {
		go i.startRoundTimerLoop()
		i.initialized = true
		i.Logger.Debug("iBFT instance init finished")
	})
}

// State returns instance state
func (i *Instance) State() *qbft.State {
	return i.state
}

// Containers returns map of containers
func (i *Instance) Containers() map[specqbft.MessageType]msgcont.MessageContainer {
	return i.containersMap
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
	if inputValue == nil {
		return errors.New("input value is nil")
	}

	messageID := message.ToMessageID(i.State().GetIdentifier())
	i.Logger.Info("Node is starting iBFT instance", zap.String("Lambda", hex.EncodeToString(i.State().GetIdentifier())))
	i.State().InputValue.Store(inputValue)
	i.State().Round.Store(specqbft.Round(1)) // start from 1
	metricsIBFTRound.WithLabelValues(messageID.GetRoleType().String(), hex.EncodeToString(messageID.GetPubKey())).Set(1)

	i.Logger.Debug("state", zap.Uint64("height", uint64(i.State().GetHeight())), zap.Uint64("round", uint64(i.State().GetRound())))
	if i.IsLeader() {
		go func() {
			i.Logger.Info("Node is leader for round 1")
			//i.ProcessStageChange(qbft.RoundStatePrePrepare) we need to process the proposal msg in order to broadcast to prepare msg

			// LeaderPreprepareDelaySeconds waits to let other nodes complete their instance start or round change.
			// Waiting will allow a more stable msg receiving for all parties.
			time.Sleep(time.Duration(i.Config.LeaderPreprepareDelaySeconds))

			msg, err := i.generatePrePrepareMessage(&specqbft.ProposalData{
				Data: i.State().GetInputValue(),
			})
			if err != nil {
				i.Logger.Warn("failed to generate pre-prepare message", zap.Error(err))
				return
			}

			if err := i.SignAndBroadcast(&msg); err != nil {
				i.Logger.Error("could not broadcast pre-prepare", zap.Error(err))
			}
		}()
	}
	i.ResetRoundTimer() // TODO could be race condition with message process?
	return nil
}

// ForceDecide will attempt to decide the instance with provided decided signed msg.
func (i *Instance) ForceDecide(msg *specqbft.SignedMessage) {
	i.Logger.Info("trying to force instance decision.")
	if err := i.DecidedMsgPipeline().Run(msg); err != nil {
		i.Logger.Error("force decided pipeline error", zap.Error(err))
	}
}

// Stop will trigger a stopped for the entire instance
func (i *Instance) Stop() {
	// stop can be run just once
	i.runStopOnce.Do(func() {
		i.stop()
		i.cancelCtx()
	})
}

// stop stops the instance
func (i *Instance) stop() {
	i.Logger.Info("stopping iBFT instance...")
	i.Logger.Debug("STOPPING IBFTController -> set stopped to true")
	i.stopped.Store(true)
	i.Logger.Debug("STOPPING IBFTController -> kill round timer")
	i.roundTimer.Kill()
	i.Logger.Debug("STOPPING IBFTController -> stopped round timer")
	i.ProcessStageChange(qbft.RoundStateStopped)
	i.Logger.Debug("STOPPING IBFTController -> round stage set stopped")
	// stop stage chan
	if i.stageChangedChan != nil {
		i.Logger.Debug("STOPPING IBFTController -> lock stage chan")
		i.stageChanCloseChan.Lock() // in order to prevent from sending to a close chan
		i.Logger.Debug("STOPPING IBFTController -> closing stage changed chan")
		close(i.stageChangedChan)
		i.Logger.Debug("STOPPING IBFTController -> closed stageChangedChan")
		i.stageChangedChan = nil
		i.Logger.Debug("STOPPING IBFTController -> stageChangedChan nilled")
		i.stageChanCloseChan.Unlock()
		i.Logger.Debug("STOPPING IBFTController -> stageChangedChan chan unlocked")
	}
	i.Logger.Info("stopped iBFT instance")
}

// Stopped is stopping queue work
func (i *Instance) Stopped() bool {
	return i.stopped.Load()
}

// ProcessMsg will process the message
func (i *Instance) ProcessMsg(msg *specqbft.SignedMessage) (bool, error) {
	var pp pipelines.SignedMessagePipeline
	var errPrefix string // TODO(nkryuchkov): make similar in ssv-spec

	switch msg.Message.MsgType {
	case specqbft.ProposalMsgType:
		pp = i.PrePrepareMsgPipeline()
		errPrefix = "proposal invalid"
	case specqbft.PrepareMsgType:
		pp = i.PrepareMsgPipeline()
		errPrefix = "invalid prepare msg"
	case specqbft.CommitMsgType:
		pp = i.CommitMsgPipeline()
		errPrefix = "commit msg invalid"
	case specqbft.RoundChangeMsgType:
		pp = i.ChangeRoundMsgPipeline()
		errPrefix = "round change msg invalid"
	default:
		i.Logger.Warn("undefined message type", zap.Any("msg", msg))
		return false, errors.Errorf("undefined message type")
	}
	if err := pp.Run(msg); err != nil {
		return false, fmt.Errorf("%s: %w", errPrefix, err)
	}

	if i.State().Stage.Load() == int32(qbft.RoundStateDecided) { // TODO better way to compare? (:Niv)
		return true, nil // TODO that's the right decidedValue? (:Niv)
	}
	return false, nil
}

// BumpRound is used to set bump round by 1
func (i *Instance) BumpRound() {
	i.bumpToRound(i.State().GetRound() + 1)
}

func (i *Instance) bumpToRound(round specqbft.Round) {
	i.processChangeRoundQuorumOnce = &sync.Once{}
	i.processPrepareQuorumOnce = &sync.Once{}
	newRound := round
	i.State().Round.Store(newRound)
	messageID := message.ToMessageID(i.State().GetIdentifier())
	metricsIBFTRound.WithLabelValues(messageID.GetRoleType().String(), hex.EncodeToString(messageID.GetPubKey())).Set(float64(newRound))
}

// ProcessStageChange set the state's round state and pushed the new state into the state channel
func (i *Instance) ProcessStageChange(stage qbft.RoundState) {
	// in order to prevent race condition between timer timeout and decided state. once decided we need to prevent any other new state
	currentStage := i.State().Stage.Load()
	if currentStage == int32(qbft.RoundStateStopped) {
		return
	}
	if currentStage == int32(qbft.RoundStateDecided) && stage != qbft.RoundStateStopped {
		return
	}

	messageID := message.ToMessageID(i.State().GetIdentifier())
	metricsIBFTStage.WithLabelValues(messageID.GetRoleType().String(), hex.EncodeToString(messageID.GetPubKey())).Set(float64(stage))

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
		i.stageChangedChan = make(chan qbft.RoundState, 1) // buffer of 1 in order to support process stop stage right after decided
	}
	return i.stageChangedChan
}

// SignAndBroadcast checks and adds the signed message to the appropriate round state type
func (i *Instance) SignAndBroadcast(msg *specqbft.Message) error {
	i.Logger.Debug("broadcasting consensus msg",
		zap.Int("type", int(msg.MsgType)),
		zap.Int64("height", int64(msg.Height)),
		zap.Int64("round", int64(msg.Round)),
	)
	pk, err := i.ValidatorShare.OperatorSharePubKey()
	if err != nil {
		return errors.Wrap(err, "could not find operator pk for signing msg")
	}

	sigByts, err := i.ssvSigner.SignRoot(msg, spectypes.QBFTSignatureType, pk.Serialize())
	if err != nil {
		return err
	}

	signedMessage := &specqbft.SignedMessage{
		Message:   msg,
		Signature: sigByts,
		Signers:   []spectypes.OperatorID{i.ValidatorShare.NodeID},
	}

	// used for instance fast change round catchup
	if msg.MsgType == specqbft.RoundChangeMsgType {
		i.setLastChangeRoundMsg(signedMessage)
	}

	encodedMsg, err := signedMessage.Encode()
	if err != nil {
		return errors.New("failed to encode consensus message")
	}
	ssvMsg := spectypes.SSVMessage{
		MsgType: spectypes.SSVConsensusMsgType,
		MsgID:   message.ToMessageID(i.State().GetIdentifier()),
		Data:    encodedMsg,
	}
	if i.network != nil {
		return i.network.Broadcast(ssvMsg)
	}
	return errors.New("no networking, could not broadcast msg")
}

func (i *Instance) setLastChangeRoundMsg(msg *specqbft.SignedMessage) {
	_ = i.changeRoundStore.SaveLastChangeRoundMsg(msg)
}

//// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
//func (i *Instance) GetLastChangeRoundMsg() *specqbft.SignedMessage {
//	err :=  i.changeRoundStore.GetLastChangeRoundMsg()
//}

// CommittedAggregatedMsg returns a signed message for the state's committed value with the max known signatures
func (i *Instance) CommittedAggregatedMsg() (*specqbft.SignedMessage, error) {
	if i.State() == nil {
		return nil, errors.New("missing instance state")
	}
	if i.decidedMsg != nil {
		return i.decidedMsg, nil
	}
	return nil, errors.New("missing decided message")
}

// GetCommittedAggSSVMessage returns ssv msg with message.SSVDecidedMsgType and the agg commit signed msg
func (i *Instance) GetCommittedAggSSVMessage() (spectypes.SSVMessage, error) {
	decidedMsg, err := i.CommittedAggregatedMsg()
	if err != nil {
		return spectypes.SSVMessage{}, err
	}
	encodedAgg, err := decidedMsg.Encode()
	if err != nil {
		return spectypes.SSVMessage{}, errors.Wrap(err, "failed to encode agg message")
	}
	ssvMsg := spectypes.SSVMessage{
		MsgType: spectypes.SSVDecidedMsgType,
		MsgID:   message.ToMessageID(i.State().GetIdentifier()),
		Data:    encodedAgg,
	}
	return ssvMsg, nil
}

func (i *Instance) setFork(fork forks.Fork) {
	if fork == nil {
		return
	}
	i.fork = fork
	//i.fork.Apply(i)
}

func generateState(opts *Options) *qbft.State {
	var identifier, height, round, preparedRound, preparedValue atomic.Value
	height.Store(opts.Height)
	round.Store(specqbft.Round(0))
	identifier.Store(opts.Identifier[:])
	preparedRound.Store(specqbft.Round(0))
	preparedValue.Store([]byte(nil))
	iv := atomic.Value{}
	iv.Store([]byte{})
	return &qbft.State{
		Stage:         *atomic.NewInt32(int32(qbft.RoundStateNotStarted)),
		Identifier:    identifier,
		Height:        height,
		InputValue:    iv,
		Round:         round,
		PreparedRound: preparedRound,
		PreparedValue: preparedValue,
	}
}
