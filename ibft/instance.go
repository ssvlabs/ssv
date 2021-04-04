package ibft

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/leader"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/msgcont"
	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/msgqueue"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/dataval"
)

// InstanceOptions defines option attributes for the Instance
type InstanceOptions struct {
	Logger         *zap.Logger
	Me             *proto.Node
	Network        network.Network
	Consensus      dataval.Validator
	LeaderSelector leader.Selector
	Params         *proto.InstanceParams
	Lambda         []byte
	PreviousLambda []byte
}

// Instance defines the instance attributes
type Instance struct {
	Me               *proto.Node
	State            *proto.State
	network          network.Network
	Consensus        dataval.Validator
	LeaderSelector   leader.Selector
	Params           *proto.InstanceParams
	roundChangeTimer *time.Timer
	Logger           *zap.Logger
	msgLock          sync.Mutex

	// messages
	msgQueue            *msgqueue.MessageQueue
	PrePrepareMessages  msgcont.MessageContainer
	PrepareMessages     msgcont.MessageContainer
	commitMessages      msgcont.MessageContainer
	ChangeRoundMessages msgcont.MessageContainer

	// channels
	changeRoundChan       chan bool
	stageChangedChans     []chan proto.RoundState
	stageChangedChansLock sync.Mutex
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
			Stage:          proto.RoundState_NotStarted,
			Lambda:         opts.Lambda,
			PreviousLambda: opts.PreviousLambda,
		},
		network:        opts.Network,
		Consensus:      opts.Consensus,
		LeaderSelector: opts.LeaderSelector,
		Params:         opts.Params,
		Logger:         opts.Logger.With(zap.Uint64("node_id", opts.Me.IbftId)),
		msgLock:        sync.Mutex{},

		msgQueue:            msgqueue.New(),
		PrePrepareMessages:  msgcontinmem.New(),
		PrepareMessages:     msgcontinmem.New(),
		commitMessages:      msgcontinmem.New(),
		ChangeRoundMessages: msgcontinmem.New(),

		changeRoundChan: make(chan bool),
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
	if i.State.PreviousLambda == nil {
		i.Logger.Fatal("can't start instance with invalid Previous Lambda")
	}

	i.Logger.Info("Node is starting iBFT instance", zap.String("Lambda", hex.EncodeToString(i.State.Lambda)))
	i.State.Round = 1      // start from 1
	i.msgQueue.SetRound(1) // start from 1
	i.State.InputValue = inputValue

	if i.IsLeader() {
		go func() {
			i.Logger.Info("Node is leader for round 1")
			i.SetStage(proto.RoundState_PrePrepare)

			// LeaderPreprepareDelay waits to let other nodes complete their instance start or round change.
			// Waiting will allow a more stable msg receiving for all parties.
			time.Sleep(time.Duration(i.Params.ConsensusParams.LeaderPreprepareDelay))

			msg := &proto.Message{
				Type:           proto.RoundState_PrePrepare,
				Round:          i.State.Round,
				Lambda:         i.State.Lambda,
				PreviousLambda: i.State.PreviousLambda,
				Value:          i.State.InputValue,
			}
			//
			if err := i.SignAndBroadcast(msg); err != nil {
				i.Logger.Fatal("could not broadcast pre-prepare", zap.Error(err))
			}
		}()
	}

	i.triggerRoundChangeOnTimer()
}

// Stage returns the instance message state
func (i *Instance) Stage() proto.RoundState {
	return i.State.Stage
}

// BumpRound is used to set round in the instance's queue - the message broker
func (i *Instance) BumpRound(round uint64) {
	i.State.Round = round
	i.msgQueue.SetRound(round)
	i.LeaderSelector.Bump()
}

// SetStage set the State's round State and pushed the new State into the State channel
func (i *Instance) SetStage(stage proto.RoundState) {
	i.State.Stage = stage

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
	msgChan := i.network.ReceivedMsgChan(i.Me.IbftId, i.State.Lambda)
	go func() {
		for {
			select {
			case msg := <-msgChan:
				i.msgQueue.AddMessage(msg)
			// Change round is called when no Quorum was achieved within a time duration
			case <-i.changeRoundChan:
				go i.uponChangeRoundTrigger()
			}
		}
	}()
}

// StartMessagePipeline - the iBFT instance is message driven with an 'upon' logic.
// each message type has it's own pipeline of checks and actions, called by the networker implementation.
// Internal chan monitor if the instance reached decision or if a round change is required.
func (i *Instance) StartMessagePipeline() {
	i.msgQueue.Subscribe(func(msg *proto.SignedMessage) {
		if msg == nil {
			time.Sleep(time.Millisecond * 100)
			return
		}

		var pp pipeline.Pipeline
		switch msg.Message.Type {
		case proto.RoundState_PrePrepare:
			pp = i.prePrepareMsgPipeline()
		case proto.RoundState_Prepare:
			pp = i.prepareMsgPipeline()
		case proto.RoundState_Commit:
			pp = i.commitMsgPipeline()
		case proto.RoundState_ChangeRound:
			pp = i.changeRoundMsgPipeline()
		default:
			i.Logger.Warn("undefined message type", zap.Any("msg", msg))
			return
		}

		if err := pp.Run(msg); err != nil {
			i.Logger.Error("msg pipeline error", zap.Error(err))
		}
	})
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
		return i.network.Broadcast(i.State.GetLambda(), signedMessage)
	}

	switch msg.Type {
	case proto.RoundState_PrePrepare:
		i.PrePrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Prepare:
		i.PrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Commit:
		i.commitMessages.AddMessage(signedMessage)
	case proto.RoundState_ChangeRound:
		i.ChangeRoundMessages.AddMessage(signedMessage)
	}

	return nil
}

// Cleanup -- TODO: describe
func (i *Instance) Cleanup() {
	// TODO: Implement
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
