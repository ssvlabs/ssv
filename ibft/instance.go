package ibft

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/msgqueue"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/bloxapp/ssv/ibft/msgcont"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/val"
	"github.com/bloxapp/ssv/network"
)

type InstanceOptions struct {
	Logger    *zap.Logger
	Me        *proto.Node
	Network   network.Network
	Consensus val.ValueValidator
	Params    *proto.InstanceParams
}

type Instance struct {
	Me               *proto.Node
	State            *proto.State
	network          network.Network
	consensus        val.ValueValidator
	params           *proto.InstanceParams
	roundChangeTimer *time.Timer
	logger           *zap.Logger
	msgLock          sync.Mutex

	// messages
	msgQueue            *msgqueue.MessageQueue
	prePrepareMessages  *msgcont.MessagesContainer
	prepareMessages     *msgcont.MessagesContainer
	commitMessages      *msgcont.MessagesContainer
	changeRoundMessages *msgcont.MessagesContainer

	// channels
	changeRoundChan  chan bool
	stageChangedChan chan proto.RoundState
}

// NewInstance is the constructor of Instance
func NewInstance(opts InstanceOptions) *Instance {
	// make sure secret key is not nil, otherwise the node can't operate
	if opts.Me.Sk == nil || len(opts.Me.Sk) == 0 {
		opts.Logger.Fatal("can't create Instance with invalid secret key")
		return nil
	}

	return &Instance{
		Me:        opts.Me,
		State:     &proto.State{Stage: proto.RoundState_NotStarted},
		network:   opts.Network,
		consensus: opts.Consensus,
		params:    opts.Params,
		logger:    opts.Logger.With(zap.Uint64("node_id", opts.Me.IbftId)),
		msgLock:   sync.Mutex{},

		msgQueue:            msgqueue.New(),
		prePrepareMessages:  msgcont.NewMessagesContainer(),
		prepareMessages:     msgcont.NewMessagesContainer(),
		commitMessages:      msgcont.NewMessagesContainer(),
		changeRoundMessages: msgcont.NewMessagesContainer(),

		changeRoundChan:  make(chan bool),
		stageChangedChan: make(chan proto.RoundState),
	}
}

/**
### Algorithm 1 IBFT pseudocode for process pi: constants, State variables, and ancillary procedures
 procedure Start(λ, value)
 	λi ← λ
 	ri ← 1
 	pri ← ⊥
 	pvi ← ⊥
 	inputV aluei ← value
 	if leader(hi, ri) = pi then
 		broadcast ⟨PRE-PREPARE, λi, ri, inputV aluei⟩ message
 		set timeri to running and expire after t(ri)
*/
func (i *Instance) Start(previousLambda, lambda, inputValue []byte) {
	i.logger.Info("Node is starting iBFT instance", zap.String("lambda", hex.EncodeToString(lambda)))
	i.BumpRound(1)
	i.State.Lambda = lambda
	i.State.PreviousLambda = previousLambda
	i.State.InputValue = inputValue

	if i.IsLeader() {
		i.logger.Info("Node is leader for round 1")
		i.SetStage(proto.RoundState_PrePrepare)

		msg := &proto.Message{
			Type:           proto.RoundState_PrePrepare,
			Round:          i.State.Round,
			Lambda:         i.State.Lambda,
			PreviousLambda: previousLambda,
			Value:          i.State.InputValue,
		}

		if err := i.SignAndBroadcast(msg); err != nil {
			i.logger.Fatal("could not broadcast pre-prepare", zap.Error(err))
		}
	}

	i.triggerRoundChangeOnTimer()
}

func (i *Instance) Stage() proto.RoundState {
	return i.State.Stage
}

func (i *Instance) BumpRound(round uint64) {
	i.State.Round = round
	i.msgQueue.SetRound(round)
}

// SetStage set the State's round State and pushed the new State into the State channel
func (i *Instance) SetStage(stage proto.RoundState) {
	i.State.Stage = stage

	// Non blocking send to channel
	select {
	case i.stageChangedChan <- stage:
		return
	default:
		return
	}
}

func (i *Instance) GetStageChan() chan proto.RoundState {
	return i.stageChangedChan
}

// StartEventLoop - starts the main event loop for the instance.
// Events are messages our timer that change the behaviour of the instance upon triggering them
func (i *Instance) StartEventLoop(lambda []byte) {
	msgChan := i.network.ReceivedMsgChan(i.Me.IbftId, lambda)
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
	go func() {
		for {
			msg := i.msgQueue.PopMessage()
			if msg == nil {
				time.Sleep(time.Millisecond * 100)
				continue
			}

			var err error
			switch msg.Message.Type {
			case proto.RoundState_PrePrepare:
				err = i.prePrepareMsgPipeline().Run(msg)
			case proto.RoundState_Prepare:
				err = i.prepareMsgPipeline().Run(msg)
			case proto.RoundState_Commit:
				err = i.commitMsgPipeline().Run(msg)
			case proto.RoundState_ChangeRound:
				err = i.changeRoundMsgPipeline().Run(msg)
			}

			if err != nil {
				i.logger.Error("msg pipeline error", zap.Error(err))
			}
		}
	}()
}

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
		IbftId:    i.Me.IbftId,
	}
	if i.network != nil {
		return i.network.Broadcast(i.State.GetLambda(), signedMessage)
	}

	switch msg.Type {
	case proto.RoundState_PrePrepare:
		i.prePrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Prepare:
		i.prepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Commit:
		i.commitMessages.AddMessage(signedMessage)
	case proto.RoundState_ChangeRound:
		i.changeRoundMessages.AddMessage(signedMessage)
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
	roundTimeout := uint64(i.params.ConsensusParams.RoundChangeDuration) * mathutil.PowerOf2(i.State.Round)
	i.roundChangeTimer = time.NewTimer(time.Duration(roundTimeout))
	i.logger.Info("started timeout clock", zap.Float64("seconds", time.Duration(roundTimeout).Seconds()))
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
