package ibft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/leader"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/msgcont"
	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
)

// InstanceOptions defines option attributes for the Instance
type InstanceOptions struct {
	Logger         *zap.Logger
	Me             *proto.Node
	Network        network.Network
	Queue          *msgqueue.MessageQueue
	ValueCheck     valcheck.ValueCheck
	LeaderSelector leader.Selector
	Params         *proto.InstanceParams
	Lambda         []byte
	SeqNumber      uint64
	ValidatorPK    []byte
}

// Instance defines the instance attributes
type Instance struct {
	Me               *proto.Node
	State            *proto.State
	network          network.Network
	ValueCheck       valcheck.ValueCheck
	LeaderSelector   leader.Selector
	Params           *proto.InstanceParams
	roundChangeTimer *time.Timer
	Logger           *zap.Logger

	// messages
	MsgQueue            *msgqueue.MessageQueue
	PrePrepareMessages  msgcont.MessageContainer
	PrepareMessages     msgcont.MessageContainer
	CommitMessages      msgcont.MessageContainer
	ChangeRoundMessages msgcont.MessageContainer

	// channels
	changeRoundChan   chan bool
	stageChangedChans []chan proto.RoundState

	// flags
	stop bool

	// locks
	stageChangedChansLock sync.Mutex
	stopLock              sync.Mutex
	stageLock             sync.Mutex
}

// NewInstance is the constructor of Instance
func NewInstance(opts InstanceOptions) *Instance {
	// make sure secret key is not nil, otherwise the node can't operate
	if opts.Me.Sk == nil || len(opts.Me.Sk) == 0 {
		opts.Logger.Fatal("can't create Instance with invalid secret key")
		return nil
	}

	return &Instance{
		Me: opts.Me,
		State: &proto.State{
			Stage:       proto.RoundState_NotStarted,
			Lambda:      opts.Lambda,
			SeqNumber:   opts.SeqNumber,
			ValidatorPk: opts.ValidatorPK,
		},
		network:        opts.Network,
		ValueCheck:     opts.ValueCheck,
		LeaderSelector: opts.LeaderSelector,
		Params:         opts.Params,
		Logger:         opts.Logger.With(zap.Uint64("node_id", opts.Me.IbftId), zap.Uint64("seq_num", opts.SeqNumber)),

		MsgQueue:            opts.Queue,
		PrePrepareMessages:  msgcontinmem.New(uint64(opts.Params.ThresholdSize())),
		PrepareMessages:     msgcontinmem.New(uint64(opts.Params.ThresholdSize())),
		CommitMessages:      msgcontinmem.New(uint64(opts.Params.ThresholdSize())),
		ChangeRoundMessages: msgcontinmem.New(uint64(opts.Params.ThresholdSize())),

		// chan
		changeRoundChan:   make(chan bool),
		stageChangedChans: make([]chan proto.RoundState, 0),

		// locks
		stopLock:              sync.Mutex{},
		stageLock:             sync.Mutex{},
		stageChangedChansLock: sync.Mutex{},
	}
}

// Start implements the Algorithm 1 IBFT pseudocode for process pi: constants, State variables, and ancillary procedures
// procedure Start(λ, value)
// 	λi ← λ
// 	ri ← 1
// 	pri ← ⊥
// 	pvi ← ⊥
// 	inputV aluei ← value
// 	if leader(hi, ri) = pi then
// 		broadcast ⟨PRE-PREPARE, λi, ri, inputV aluei⟩ message
// 		set timer to running and expire after t(ri)
func (i *Instance) Start(inputValue []byte) {
	if i.State.Lambda == nil {
		i.Logger.Fatal("can't start instance with invalid Lambda")
	}

	i.Logger.Info("Node is starting iBFT instance", zap.String("Lambda", hex.EncodeToString(i.State.Lambda)))
	i.State.Round = 1 // start from 1
	i.State.InputValue = inputValue

	if i.IsLeader() {
		go func() {
			i.Logger.Info("Node is leader for round 1")
			i.SetStage(proto.RoundState_PrePrepare)

			// LeaderPreprepareDelay waits to let other nodes complete their instance start or round change.
			// Waiting will allow a more stable msg receiving for all parties.
			time.Sleep(time.Duration(i.Params.ConsensusParams.LeaderPreprepareDelay))

			msg := i.generatePrePrepareMessage(i.State.InputValue)
			//
			if err := i.SignAndBroadcast(msg); err != nil {
				i.Logger.Fatal("could not broadcast pre-prepare", zap.Error(err))
			}
		}()
	}
	i.triggerRoundChangeOnTimer()
}

// Stop will trigger a stop for the entire instance
func (i *Instance) Stop() {
	i.stopLock.Lock()
	defer i.stopLock.Unlock()

	i.stop = true
	i.stopRoundChangeTimer()
	i.SetStage(proto.RoundState_Stopped)
	i.Logger.Info("stopping iBFT instance...")
}

// IsStopped returns true if msg stopped, false if not
func (i *Instance) IsStopped() bool {
	i.stopLock.Lock()
	defer i.stopLock.Unlock()

	return i.stop
}

// BumpRound is used to set round in the instance's MsgQueue - the message broker
func (i *Instance) BumpRound(round uint64) {
	i.State.Round = round
	i.LeaderSelector.Bump()
}

// Stage returns the instance message state
func (i *Instance) Stage() proto.RoundState {
	i.stageLock.Lock()
	defer i.stageLock.Unlock()
	return i.State.Stage
}

// SetStage set the State's round State and pushed the new State into the State channel
func (i *Instance) SetStage(stage proto.RoundState) {
	i.stageLock.Lock()
	defer i.stageLock.Unlock()

	i.State.Stage = stage

	// Delete all queue messages when decided, we do not need them anymore.
	if i.State.Stage == proto.RoundState_Decided || i.State.Stage == proto.RoundState_Stopped {
		for j := uint64(1); j <= i.State.Round; j++ {
			i.MsgQueue.PurgeIndexedMessages(msgqueue.IBFTRoundIndexKey(i.State.Lambda, j))
		}
	}

	// Non blocking send to channel
	for _, ch := range i.stageChangedChans {
		select {
		case ch <- stage:
		default:
		}
	}
}

// GetStageChan returns a RoundState channel added to the stateChangesChans array
func (i *Instance) GetStageChan() chan proto.RoundState {
	ch := make(chan proto.RoundState)
	i.stageChangedChansLock.Lock()
	i.stageChangedChans = append(i.stageChangedChans, ch)
	i.stageChangedChansLock.Unlock()
	return ch
}

// StartEventLoop - starts the main event loop for the instance.
// Events are messages our timer that change the behaviour of the instance upon triggering them
func (i *Instance) StartEventLoop() {
	for range i.changeRoundChan {
		go i.uponChangeRoundTrigger()
	}
}

// StartMessagePipeline - the iBFT instance is message driven with an 'upon' logic.
// each message type has it's own pipeline of checks and actions, called by the networker implementation.
// Internal chan monitor if the instance reached decision or if a round change is required.
func (i *Instance) StartMessagePipeline() {
	for {
		if i.IsStopped() {
			i.Logger.Info("stopping iBFT message pipeline...")
			break
		}

		processedMsg, err := i.ProcessMessage()
		if err != nil {
			i.Logger.Error("msg pipeline error", zap.Error(err))
		}
		if !processedMsg {
			time.Sleep(time.Millisecond * 100)
		}

		// In case instance was decided, exit message pipeline
		if i.Stage() == proto.RoundState_Decided {
			break
		}
	}
}

// SignAndBroadcast checks and adds the signed message to the appropriate round state type
func (i *Instance) SignAndBroadcast(msg *proto.Message) error {
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(i.Me.Sk); err != nil { // TODO - cache somewhere
		return err
	}

	sig, err := msg.Sign(sk)
	if err != nil {
		return err
	}

	signedMessage := &proto.SignedMessage{
		Message:   msg,
		Signature: sig.Serialize(),
		SignerIds: []uint64{i.Me.IbftId},
	}
	if i.network != nil {
		return i.network.Broadcast(signedMessage)
	}

	switch msg.Type {
	case proto.RoundState_PrePrepare:
		i.PrePrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Prepare:
		i.PrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Commit:
		i.CommitMessages.AddMessage(signedMessage)
	case proto.RoundState_ChangeRound:
		i.ChangeRoundMessages.AddMessage(signedMessage)
	}

	return nil
}

/**
"Timer:
	In addition to the State variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the State changes to expired."
*/
func (i *Instance) triggerRoundChangeOnTimer() {
	// make sure previous timer is stopped
	i.stopRoundChangeTimer()

	// stat new timer
	roundTimeout := uint64(i.Params.ConsensusParams.RoundChangeDuration) * mathutil.PowerOf2(i.State.Round)
	i.roundChangeTimer = time.NewTimer(time.Duration(roundTimeout))
	i.Logger.Info("started timeout clock", zap.Float64("seconds", time.Duration(roundTimeout).Seconds()))
	go func() {
		<-i.roundChangeTimer.C
		i.changeRoundChan <- true
		i.stopRoundChangeTimer()
	}()
}

func (i *Instance) stopRoundChangeTimer() {
	if i.roundChangeTimer != nil {
		i.roundChangeTimer.Stop()
		i.roundChangeTimer = nil
	}
}
