package ibft

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/networker"
)

func Place() {
	att := eth.Attestation{}
	att.String()

	blk := eth.BeaconBlock{}
	blk.String()
}

type Instance struct {
	me               *types.Node
	state            *types.State
	network          networker.Networker
	implementation   types.Implementor
	params           *types.InstanceParams
	roundChangeTimer *time.Timer
	logger           *zap.Logger
	msgLock          sync.Mutex

	// messages
	prePrepareMessages  *types.MessagesContainer
	prepareMessages     *types.MessagesContainer
	commitMessages      *types.MessagesContainer
	changeRoundMessages *types.MessagesContainer

	// flags
	decided     chan bool
	changeRound chan bool
}

// New is the constructor of Instance
func New(
	logger *zap.Logger,
	me *types.Node,
	network networker.Networker,
	implementation types.Implementor,
	params *types.InstanceParams,
) *Instance {
	// make sure secret key is not nil, otherwise the node can't operate
	if me.Sk == nil || len(me.Sk) == 0 {
		logrus.Fatalf("can't create Instance with invalid secret key")
		return nil
	}

	return &Instance{
		me:             me,
		state:          &types.State{Stage: types.RoundState_NotStarted},
		network:        network,
		implementation: implementation,
		params:         params,
		logger:         logger.With(zap.Uint64("node_id", me.IbftId)),
		msgLock:        sync.Mutex{},

		prePrepareMessages:  types.NewMessagesContainer(),
		prepareMessages:     types.NewMessagesContainer(),
		commitMessages:      types.NewMessagesContainer(),
		changeRoundMessages: types.NewMessagesContainer(),

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
func (i *Instance) Start(lambda []byte, inputValue []byte) error {
	i.logger.Info("Node is starting iBFT instance", zap.String("lambda", hex.EncodeToString(lambda)))
	i.state.Round = 1
	i.state.Lambda = lambda
	i.state.PreviousLambda = previousLambda
	i.state.InputValue = inputValue

	if i.implementation.IsLeader(i.state) {
		i.logger.Info("Node is leader for round 1")
		i.state.Stage = types.RoundState_PrePrepare
		msg := &types.Message{
			Type:           types.RoundState_PrePrepare,
			Round:          i.state.Round,
			Lambda:         i.state.Lambda,
			PreviousLambda: previousLambda,
			Value:          i.state.InputValue,
		}
		if err := i.SignAndBroadcast(msg); err != nil {
			return err
		}
	}
	i.triggerRoundChangeOnTimer()
	return nil
}

// Committed returns a channel which indicates when this instance of iBFT decided an input value.
func (i *Instance) Committed() chan bool {
	return i.decided
}

// StartEventLoopAndMessagePipeline - the iBFT instance is message driven with an 'upon' logic.
// each message type has it's own pipeline of checks and actions, called by the networker implementation.
// Internal chan monitor if the instance reached decision or if a round change is required.
func (i *Instance) StartEventLoopAndMessagePipeline() {
	lockMsg := func(signedMsg *types.SignedMessage) error {
		//i.msgLock.Lock()
		return nil
	}
	unlockMsg := func(signedMsg *types.SignedMessage) error {
		//i.msgLock.Unlock()
		return nil
	}

	i.network.SetMessagePipeline(i.me.IbftId, types.RoundState_PrePrepare, []types.PipelineFunc{
		lockMsg,
		MsgTypeCheck(types.RoundState_PrePrepare),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validatePrePrepareMsg(),
		i.uponPrePrepareMsg(),
		unlockMsg,
	})
	i.network.SetMessagePipeline(i.me.IbftId, types.RoundState_Prepare, []types.PipelineFunc{
		lockMsg,
		MsgTypeCheck(types.RoundState_Prepare),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validatePrepareMsg(),
		i.uponPrepareMsg(),
		unlockMsg,
	})
	i.network.SetMessagePipeline(i.me.IbftId, types.RoundState_Commit, []types.PipelineFunc{
		lockMsg,
		MsgTypeCheck(types.RoundState_Commit),
		i.ValidateLambdas(),
		i.ValidateRound(),
		i.AuthMsg(),
		i.validateCommitMsg(),
		i.uponCommitMsg(),
		unlockMsg,
	})
	i.network.SetMessagePipeline(i.me.IbftId, types.RoundState_ChangeRound, []types.PipelineFunc{
		lockMsg,
		MsgTypeCheck(types.RoundState_ChangeRound),
		i.ValidateLambdas(),
		i.ValidateRound(), // TODO - should we validate round? or maybe just higher round?
		i.AuthMsg(),
		i.validateChangeRoundMsg(),
		i.uponChangeRoundMsg(),
		unlockMsg,
	})

	go func() {
		for {
			select {
			// When decided is triggered the iBFT instance has concluded and should stop.
			case <-i.decided:
				i.logger.Info("iBFT instance decided, exiting..")
				//close(msgChan) // TODO - find a safe way to close connection
				//return
			// Change round is called when no Quorum was achieved within a time duration
			case <-i.changeRound:
				go i.uponChangeRoundTrigger()
			}
		}
	}()
}

func (i *Instance) SignAndBroadcast(msg *types.Message) error {
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(i.me.Sk); err != nil { // TODO - cache somewhere
		return err
	}

	sig, err := msg.Sign(sk)
	if err != nil {
		return err
	}

	signedMessage := &types.SignedMessage{
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
	i.logger.Info("started timeout clock", zap.Float64("seconds", time.Duration(roundTimeout).Seconds()))
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
