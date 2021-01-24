package ibft

import (
	"encoding/hex"
	"time"

	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
)

func Place() {
	att := eth.Attestation{}
	att.String()

	blk := eth.BeaconBlock{}
	blk.String()
}

type iBFTInstance struct {
	state            *types.State
	network          types.Networker
	implementation   types.Implementor
	params           *types.Params
	roundChangeTimer *time.Timer
	log              *zap.Logger

	// messages
	prePrepareMessages  *types.MessagesContainer
	prepareMessages     *types.MessagesContainer
	commitMessages      *types.MessagesContainer
	roundChangeMessages *types.MessagesContainer

	// flags
	started     bool
	decided     chan bool
	changeRound chan bool
}

// New is the constructor of iBFTInstance
func New(logger *zap.Logger, nodeId uint64, network types.Networker, implementation types.Implementor, params *types.Params) *iBFTInstance {
	return &iBFTInstance{
		state:          &types.State{IBFTId: nodeId},
		network:        network,
		implementation: implementation,
		params:         params,
		log:            logger.With(zap.Uint64("node_id", nodeId)),

		prePrepareMessages:  types.NewMessagesContainer(),
		prepareMessages:     types.NewMessagesContainer(),
		commitMessages:      types.NewMessagesContainer(),
		roundChangeMessages: types.NewMessagesContainer(),

		started:     false,
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
func (i *iBFTInstance) Start(lambda []byte, inputValue []byte) error {
	i.log.Info("Node is starting iBFT instance", zap.String("instance", hex.EncodeToString(lambda)))
	i.state.Round = 1
	i.state.Lambda = lambda
	i.state.InputValue = inputValue
	i.state.Stage = types.RoundState_Preprepare

	if i.implementation.IsLeader(i.state) {
		i.log.Info("Node is leader for round 1")
		msg := &types.Message{
			Type:       types.RoundState_Preprepare,
			Round:      i.state.Round,
			Lambda:     i.state.Lambda,
			InputValue: i.state.InputValue,
			IbftId:     i.state.IBFTId,
		}
		if err := i.network.Broadcast(msg); err != nil {
			return err
		}
	}
	i.started = true
	i.triggerRoundChangeOnTimer()
	return nil
}

// Committed returns a channel which indicates when this instance of iBFT decided an input value.
func (i *iBFTInstance) Committed() chan bool {
	return i.decided
}

func (i *iBFTInstance) StartEventLoop() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for {
			select {
			// When a new msg is received, we act upon it to progress in the protocol
			case msg := <-msgChan:
				// TODO - check iBFT instance (lambda)
				switch msg.Type {
				case types.RoundState_Preprepare:
					go i.uponPrePrepareMessage(msg)
				case types.RoundState_Prepare:
					go i.uponPrepareMessage(msg)
				case types.RoundState_Commit:
					go i.uponCommitMessage(msg)
				case types.RoundState_RoundChange:
					go i.uponChangeRoundMessage(msg)
					//case types.MsgType_Decide:
					//	// TODO
					//	continue
				}
			// When decided is triggered the iBFT instance has concluded and should stop.
			case <-i.decided:
				i.log.Info("iBFT instance decided, exiting..")
				//close(msgChan) // TODO - find a safe way to close connection
				//return
			// Change round is called when no Quorum was achieved within a time duration
			case <-i.changeRound:
				go i.uponChangeRoundTrigger()
			}
		}
	}()
}

/**
"Timer:
	In addition to the state variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the state changes to expired."
*/
func (i *iBFTInstance) triggerRoundChangeOnTimer() {
	// make sure previous timer is stopped
	i.stopRoundChangeTimer()

	// stat new timer
	roundTimeout := uint64(i.params.RoundChangeDuration) * mathutil.PowerOf2(i.state.Round)
	i.roundChangeTimer = time.NewTimer(time.Duration(roundTimeout))
	go func() {
		<-i.roundChangeTimer.C
		i.changeRound <- true
		i.stopRoundChangeTimer()
	}()
}

func (i *iBFTInstance) stopRoundChangeTimer() {
	if i.roundChangeTimer != nil {
		i.roundChangeTimer.Stop()
		i.roundChangeTimer = nil
	}
}
