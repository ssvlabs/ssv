package ibft

import (
	"encoding/hex"
	"errors"
	"github.com/bloxapp/ssv/ibft/eventqueue"
	"github.com/bloxapp/ssv/ibft/roundtimer"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/validator/storage"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/leader"

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
	ValidatorShare *storage.Share
	Network        network.Network
	Queue          *msgqueue.MessageQueue
	ValueCheck     valcheck.ValueCheck
	LeaderSelector leader.Selector
	Config         *proto.InstanceConfig
	Lambda         []byte
	SeqNumber      uint64
	// RequireMinPeers flag to require minimum peers before starting an instance
	// useful for tests where we want (sometimes) to avoid networking
	RequireMinPeers bool
}

// Instance defines the instance attributes
type Instance struct {
	ValidatorShare *storage.Share
	State          *proto.State
	network        network.Network
	ValueCheck     valcheck.ValueCheck
	LeaderSelector leader.Selector
	Config         *proto.InstanceConfig
	roundTimer     *roundtimer.RoundTimer
	Logger         *zap.Logger

	// messages
	MsgQueue            *msgqueue.MessageQueue
	PrePrepareMessages  msgcont.MessageContainer
	PrepareMessages     msgcont.MessageContainer
	CommitMessages      msgcont.MessageContainer
	ChangeRoundMessages msgcont.MessageContainer
	lastBroadcastedMsg  *proto.SignedMessage

	// event loop
	eventQueue eventqueue.EventQueue

	// channels
	stageChangedChan chan proto.RoundState

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
	stageChangedChansLock        sync.Mutex
	stageLock                    sync.Mutex
	stateLock                    sync.Mutex
	lastMsgLock                  sync.RWMutex
}

// NewInstance is the constructor of Instance
func NewInstance(opts InstanceOptions) *Instance {
	return &Instance{
		ValidatorShare: opts.ValidatorShare,
		State: &proto.State{
			Stage:     proto.RoundState_NotStarted,
			Lambda:    opts.Lambda,
			SeqNumber: opts.SeqNumber,
		},
		network:        opts.Network,
		ValueCheck:     opts.ValueCheck,
		LeaderSelector: opts.LeaderSelector,
		Config:         opts.Config,
		Logger: opts.Logger.With(zap.Uint64("node_id", opts.ValidatorShare.NodeID),
			zap.Uint64("seq_num", opts.SeqNumber),
			zap.String("pubKey", opts.ValidatorShare.PublicKey.SerializeToHexStr())),

		MsgQueue:            opts.Queue,
		PrePrepareMessages:  msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize())),
		PrepareMessages:     msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize())),
		CommitMessages:      msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize())),
		ChangeRoundMessages: msgcontinmem.New(uint64(opts.ValidatorShare.ThresholdSize())),

		roundTimer: roundtimer.New(),

		eventQueue: eventqueue.New(),

		// locks
		runInitOnce:                  sync.Once{},
		runStopOnce:                  sync.Once{},
		processChangeRoundQuorumOnce: sync.Once{},
		processPrepareQuorumOnce:     sync.Once{},
		processCommitQuorumOnce:      sync.Once{},
		stopLock:                     sync.Mutex{},
		stageLock:                    sync.Mutex{},
		stageChangedChansLock:        sync.Mutex{},
		stateLock:                    sync.Mutex{},
		lastMsgLock:                  sync.RWMutex{},
	}
}

// Init must be called before start can be
func (i *Instance) Init() {
	i.runInitOnce.Do(func() {
		go i.StartMessagePipeline()
		go i.StartPartialChangeRoundPipeline()
		go i.startRoundTimerLoop()
		go i.StartMainEventLoop()
		i.initialized = true
		i.Logger.Debug("iBFT instance init finished")
	})
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
func (i *Instance) Start(inputValue []byte) error {
	if !i.initialized {
		return errors.New("can't start a non initialized instance")
	}
	if i.State.Lambda == nil {
		return errors.New("can't start instance with invalid Lambda")
	}

	i.Logger.Info("Node is starting iBFT instance", zap.String("Lambda", hex.EncodeToString(i.State.Lambda)))
	i.stateLock.Lock()
	i.State.Round = 1 // start from 1
	i.State.InputValue = inputValue
	i.stateLock.Unlock()

	if i.IsLeader() {
		go func() {
			i.Logger.Info("Node is leader for round 1")
			i.SetStage(proto.RoundState_PrePrepare)

			// LeaderPreprepareDelaySeconds waits to let other nodes complete their instance start or round change.
			// Waiting will allow a more stable msg receiving for all parties.
			time.Sleep(time.Duration(i.Config.LeaderPreprepareDelaySeconds))

			msg := i.generatePrePrepareMessage(i.State.InputValue)
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
func (i *Instance) ForceDecide(msg *proto.SignedMessage) {
	i.eventQueue.Add(func() {
		i.Logger.Info("trying to force instance decision.")
		if err := i.forceDecidedPipeline().Run(msg); err != nil {
			i.Logger.Error("force decided pipeline error", zap.Error(err))
		}
	})
}

// Stop will trigger a stopped for the entire instance
func (i *Instance) Stop() {
	// stop can be run just once
	i.runStopOnce.Do(func() {
		if added := i.eventQueue.Add(i.stop); !added {
			i.Logger.Debug("could not add 'stop' to event queue")
		}
	})
}

// stop stops the instance
func (i *Instance) stop() {
	i.Logger.Info("stopping iBFT instance...")
	i.stopLock.Lock()
	defer i.stopLock.Unlock()
	i.Logger.Debug("STOPPING IBFT -> pass stopLock")
	i.stopped = true
	i.roundTimer.Stop()
	i.Logger.Debug("STOPPING IBFT -> stopped round timer")
	i.SetStage(proto.RoundState_Stopped)
	i.Logger.Debug("STOPPING IBFT -> set stage to stop")
	i.eventQueue.ClearAndStop()
	i.Logger.Debug("STOPPING IBFT -> cleared event queue")

	// stop stage chan
	i.stageLock.Lock()
	defer i.stageLock.Unlock()
	i.Logger.Debug("STOPPING IBFT -> passed stageLock")
	if i.stageChangedChan != nil {
		close(i.stageChangedChan)
		i.Logger.Debug("STOPPING IBFT -> closed stageChangedChan")
		i.stageChangedChan = nil
	}

	i.Logger.Info("stopped iBFT instance")
}

// Stopped is stopping queue work
func (i *Instance) Stopped() bool {
	i.stopLock.Lock()
	defer i.stopLock.Unlock()

	return i.stopped
}

// BumpRound is used to set bump round by 1
func (i *Instance) BumpRound() {
	i.setRound(i.State.Round + 1)
}

func (i *Instance) setRound(newRound uint64) {
	i.processChangeRoundQuorumOnce = sync.Once{}
	i.processPrepareQuorumOnce = sync.Once{}
	i.processCommitQuorumOnce = sync.Once{}
	i.State.Round = newRound
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
	if stage == proto.RoundState_Decided || stage == proto.RoundState_Stopped {
		for j := uint64(1); j <= i.State.Round; j++ {
			i.MsgQueue.PurgeIndexedMessages(msgqueue.IBFTMessageIndexKey(i.State.Lambda, i.State.SeqNumber, j))
		}
	}

	// blocking send to channel
	if i.stageChangedChan != nil {
		i.stageChangedChan <- stage
	}
}

// GetStageChan returns a RoundState channel added to the stateChangesChans array
func (i *Instance) GetStageChan() chan proto.RoundState {
	if i.stageChangedChan == nil {
		i.stageChangedChan = make(chan proto.RoundState)
	}
	return i.stageChangedChan
}

// SignAndBroadcast checks and adds the signed message to the appropriate round state type
func (i *Instance) SignAndBroadcast(msg *proto.Message) error {
	sig, err := msg.Sign(i.ValidatorShare.ShareKey)
	if err != nil {
		return err
	}

	signedMessage := &proto.SignedMessage{
		Message:   msg,
		Signature: sig.Serialize(),
		SignerIds: []uint64{i.ValidatorShare.NodeID},
	}
	if i.network != nil {
		return i.network.Broadcast(i.ValidatorShare.PublicKey.Serialize(), signedMessage)
	}

	switch msg.Type {
	case proto.RoundState_PrePrepare:
		i.PrePrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Prepare:
		i.PrepareMessages.AddMessage(signedMessage)
	case proto.RoundState_Commit:
		i.CommitMessages.AddMessage(signedMessage)
	case proto.RoundState_ChangeRound:
		i.setLastBroadcastedMsg(signedMessage) // save only change round
		i.ChangeRoundMessages.AddMessage(signedMessage)
	}

	return nil
}

func (i *Instance) setLastBroadcastedMsg(msg *proto.SignedMessage) {
	i.lastMsgLock.Lock()
	defer i.lastMsgLock.Unlock()
	i.lastBroadcastedMsg = msg
}

// LastBroadcastedMsg returns the latest broadcasted msg from the instance
func (i *Instance) LastBroadcastedMsg() *proto.SignedMessage {
	i.lastMsgLock.RLock()
	defer i.lastMsgLock.RUnlock()
	return i.lastBroadcastedMsg
}
