package ibft

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/bloxapp/ssv/ibft/types"
	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
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
	log              *logrus.Entry
	roundChangeTimer *time.Timer

	// messages
	prePrepareMessages *types.MessagesContainer
	prepareMessages    *types.MessagesContainer
	commitMessages     *types.MessagesContainer

	// flags
	started     bool
	committed   chan bool
	changeRound chan bool
}

func New(nodeId uint64, network types.Networker, implementation types.Implementor, params *types.Params) *iBFTInstance {
	return &iBFTInstance{
		state:          &types.State{IBFTId: nodeId},
		network:        network,
		implementation: implementation,
		params:         params,
		log:            logrus.WithFields(logrus.Fields{"node_id": nodeId}),
		//
		prePrepareMessages: types.NewMessagesContainer(),
		prepareMessages:    types.NewMessagesContainer(),
		commitMessages:     types.NewMessagesContainer(),
		//
		started:     false,
		committed:   make(chan bool),
		changeRound: make(chan bool),
	}
}

func (i *iBFTInstance) Start(lambda []byte, inputValue []byte) error {
	i.initRound(0)
	i.state.Lambda = lambda
	i.state.InputValue = inputValue

	if i.implementation.IsLeader(i.state) {
		msg := &types.Message{
			Type:       types.MsgType_Preprepare,
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

// Committed returns a channel which indicates when this instance of iBFT committed an input value.
func (i *iBFTInstance) Committed() chan bool {
	return i.committed
}

func (i *iBFTInstance) StartEventLoop() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for {
			select {
			// When a new msg is received, we act upon it to progress in the protocol
			case msg := <-msgChan:
				switch msg.Type {
				case types.MsgType_Preprepare:
					go i.uponPrePrepareMessage(msg)
				case types.MsgType_Prepare:
					go i.uponPrepareMessage(msg)
				case types.MsgType_Commit:
					go i.uponCommitMessage(msg)
				case types.MsgType_RoundChange:
					go i.uponChangeRoundMessage(msg)
					//case types.MsgType_Decide:
					//	// TODO
					//	continue
				}
			// When committed is triggered the iBFT instance has concluded and should stop.
			case <-i.committed:
				i.log.Info("iBFT instance committed, exiting..")
				//close(msgChan) // TODO - find a safe way to close connection
				//return
			// Change round is called when no Quorum was achieved within a time duration
			case <-i.changeRound:
				go i.uponChangeRoundTrigger()
			}
		}
	}()
}

// initRound prepares the iBFT instance for a fresh round.
func (i *iBFTInstance) initRound(round uint64) {
	i.state.Round = round
}

func (i *iBFTInstance) triggerRoundChangeOnTimer() {
	// make sure previous timer is stopped
	if i.roundChangeTimer != nil {
		i.roundChangeTimer.Stop()
		i.roundChangeTimer = nil
	}

	// stat new timer
	i.roundChangeTimer = time.NewTimer(time.Duration(i.params.RoundChangeDuration))
	go func() {
		<-i.roundChangeTimer.C
		i.changeRound <- true
		i.stopRoundChangeTimer()
	}()
}

func (i *iBFTInstance) stopRoundChangeTimer() {
	i.roundChangeTimer.Stop()
	i.roundChangeTimer = nil
}
