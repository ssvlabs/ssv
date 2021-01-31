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

	"github.com/bloxapp/ssv/ibft/consensus"
	"github.com/bloxapp/ssv/network"
)

type InstanceOptions struct {
	Logger    *zap.Logger
	Me        *proto.Node
	Network   network.Network
	Consensus consensus.Consensus
	Params    *proto.InstanceParams
}

type Instance struct {
	me               *proto.Node
	state            *State
	network          network.Network
	consensus        consensus.Consensus
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

	// flags
	decided     chan bool
	changeRound chan bool
}

// NewInstance is the constructor of Instance
func NewInstance(opts InstanceOptions) *Instance {
	// make sure secret key is not nil, otherwise the node can't operate
	if opts.Me.Sk == nil || len(opts.Me.Sk) == 0 {
		opts.Logger.Fatal("can't create Instance with invalid secret key")
		return nil
	}

	return &Instance{
		me:        opts.Me,
		state:     &State{Stage: proto.RoundState_NotStarted},
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

		decided:     make(chan bool),
		changeRound: make(chan bool),
	}
}

/**
### Algorithm 1 IBFT pseudocode for process pi: constants, state variables, and ancillary procedures
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
func (i *Instance) Start(previousLambda, lambda []byte, inputValue []byte) (decided chan bool, err error) {
	i.Log("Node is starting iBFT instance", false, zap.String("lambda", hex.EncodeToString(lambda)))
	i.BumpRound(1)
	i.state.Lambda = lambda
	i.state.PreviousLambda = previousLambda
	i.state.InputValue = inputValue

	if i.IsLeader() {
		i.Log("Node is leader for round 1", false)
		i.state.Stage = proto.RoundState_PrePrepare
		msg := &proto.Message{
			Type:           proto.RoundState_PrePrepare,
			Round:          i.state.Round,
			Lambda:         i.state.Lambda,
			PreviousLambda: previousLambda,
			Value:          i.state.InputValue,
		}
		if err := i.SignAndBroadcast(msg); err != nil {
			return nil, err
		}
	}
	i.triggerRoundChangeOnTimer()
	return i.decided, nil
}

func (i *Instance) Stage() proto.RoundState {
	return i.state.Stage
}

// StartEventLoop - starts the main event loop for the instance.
// Events are messages our timer that change the behaviour of the instance upon triggering them
func (i *Instance) StartEventLoop() {
	msgChan := i.network.ReceivedMsgChan(i.me.IbftId)
	go func() {
		for {
			select {
			case msg := <-msgChan:
				i.msgQueue.AddMessage(msg)
			// When decided is triggered the iBFT instance has concluded and should stop.
			case <-i.decided:
				i.Log("iBFT instance decided, exiting..", false)
				//close(msgChan) // TODO - find a safe way to close connection
				//return
			// Change round is called when no Quorum was achieved within a time duration
			case <-i.changeRound:
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
				time.Sleep(time.Millisecond * 300)
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
				i.Log("msg pipeline error", true, zap.Error(err))
			}
		}
	}()
}

func (i *Instance) SignAndBroadcast(msg *proto.Message) error {
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(i.me.Sk); err != nil { // TODO - cache somewhere
		return err
	}

	sig, err := msg.Sign(sk)
	if err != nil {
		return err
	}

	signedMessage := &proto.SignedMessage{
		Message:   msg,
		Signature: sig.Serialize(),
		IbftId:    i.me.IbftId,
	}
	return i.network.Broadcast(signedMessage)
}

/**
"Timer:
	In addition to the state variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the state changes to expired."
*/
func (i *Instance) triggerRoundChangeOnTimer() {
	// make sure previous timer is stopped
	i.stopRoundChangeTimer()

	// stat new timer
	roundTimeout := uint64(i.params.ConsensusParams.RoundChangeDuration) * mathutil.PowerOf2(i.state.Round)
	i.roundChangeTimer = time.NewTimer(time.Duration(roundTimeout))
	i.Log("started timeout clock", false, zap.Float64("seconds", time.Duration(roundTimeout).Seconds()))
	go func() {
		<-i.roundChangeTimer.C
		i.changeRound <- true
		i.stopRoundChangeTimer()
	}()
}

func (i *Instance) stopRoundChangeTimer() {
	if i.roundChangeTimer != nil {
		i.roundChangeTimer.Stop()
		i.roundChangeTimer = nil
	}
}

func (i *Instance) IsLeader() bool {
	return i.me.IbftId == i.RoundLeader()
}

func (i *Instance) RoundLeader() uint64 {
	return i.state.Round % uint64(i.params.CommitteeSize())
}

func (i *Instance) BumpRound(round uint64) {
	i.state.Round = round
	i.msgQueue.SetRound(round)
}

func (i *Instance) Log(msg string, err bool, fields ...zap.Field) {
	if i.logger != nil {
		if err {
			i.logger.Error(msg, fields...)
		} else {
			i.logger.Info(msg, fields...)
		}
	}
}
