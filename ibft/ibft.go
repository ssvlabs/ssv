package ibft

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"strconv"
	"sync"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

// StartOptions defines type for IBFT instance options
type StartOptions struct {
	Logger         *zap.Logger
	ValueCheck     valcheck.ValueCheck
	PrevInstance   []byte
	SeqNumber      uint64
	Value          []byte
	Duty           *ethpb.DutiesResponse_Duty
	ValidatorShare storage.Share
}

// IBFT represents behavior of the IBFT
type IBFT interface {
	// Init should be called after creating an IBFT instance to init the instance, sync it, etc.
	Init()

	// StartInstance starts a new instance by the given options
	StartInstance(opts StartOptions) (bool, int, []byte)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() (uint64, error)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[uint64]*proto.Node

	// GetIdentifier returns ibft identifier made of public key and role (type)
	GetIdentifier() []byte
}

// ibftImpl implements IBFT interface
type ibftImpl struct {
	role                beacon.Role
	currentInstance     *Instance
	currentInstanceLock sync.Locker
	logger              *zap.Logger
	ibftStorage         collections.Iibft
	network             network.Network
	msgQueue            *msgqueue.MessageQueue
	instanceConfig      *proto.InstanceConfig
	ValidatorShare      *storage.Share
	Identifier          []byte
	leaderSelector      leader.Selector

	// flags
	initFinished bool
}

// New is the constructor of IBFT
func New(role beacon.Role, identifier []byte, logger *zap.Logger, storage collections.Iibft, network network.Network, queue *msgqueue.MessageQueue, instanceConfig *proto.InstanceConfig, ValidatorShare *storage.Share, ) IBFT {
	ret := &ibftImpl{
		role:                role,
		ibftStorage:         storage,
		currentInstanceLock: &sync.Mutex{},
		logger:              logger,
		network:             network,
		msgQueue:            queue,
		instanceConfig:      instanceConfig,
		ValidatorShare:      ValidatorShare,
		Identifier:          identifier,
		leaderSelector:      &leader.Deterministic{},

		// flags
		initFinished: false,
	}
	return ret
}

func (i *ibftImpl) Init() {
	i.processDecidedQueueMessages()
	i.processSyncQueueMessages()
	i.listenToSyncMessages()
	i.waitForMinPeerCount(2) // minimum of 3 validators (the current + 2)
	i.SyncIBFT()
	i.listenToNetworkMessages()
	i.listenToNetworkDecidedMessages()
	i.initFinished = true
}

func (i *ibftImpl) StartInstance(opts StartOptions) (bool, int, []byte) {
	instanceOpts := i.instanceOptionsFromStartOptions(opts)

	if err := i.canStartNewInstance(instanceOpts); err != nil {
		opts.Logger.Error("can't start new iBFT instance", zap.Error(err))
	}

	newInstance := NewInstance(instanceOpts)
	i.currentInstance = newInstance
	go newInstance.StartEventLoop()
	go newInstance.StartMessagePipeline()
	stageChan := newInstance.GetStageChan()

	err := i.resetLeaderSelection(append(i.Identifier, []byte(strconv.FormatUint(i.currentInstance.State.SeqNumber, 10))...)) // Important for deterministic leader selection
	if err != nil {
		newInstance.Logger.Error("could not reset leader selection", zap.Error(err))
		return false, 0, nil
	}
	go newInstance.Start(opts.Value)

	// Store prepared round and value and decided stage.
	for {
		switch stage := <-stageChan; stage {
		// TODO - complete values
		case proto.RoundState_Prepare:
			if err := i.ibftStorage.SaveCurrentInstance(newInstance.ValidatorShare.PublicKey.Serialize(), newInstance.State); err != nil {
				newInstance.Logger.Error("could not save prepare msg to storage", zap.Error(err))
				return false, 0, nil
			}
		case proto.RoundState_Decided:
			agg, err := newInstance.CommittedAggregatedMsg()
			if err != nil {
				newInstance.Logger.Error("could not get aggregated commit msg and save to storage", zap.Error(err))
				return false, 0, nil
			}
			if err := i.ibftStorage.SaveDecided(agg); err != nil {
				newInstance.Logger.Error("could not save aggregated commit msg to storage", zap.Error(err))
				return false, 0, nil
			}
			if err := i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
				i.logger.Error("could not save highest decided message to storage", zap.Error(err))
			}
			if err := i.network.BroadcastDecided(agg); err != nil {
				i.logger.Error("could not broadcast decided message", zap.Error(err))
			}
			i.currentInstance = nil
			return true, len(agg.GetSignerIds()), agg.Message.Value
		}
	}
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (i *ibftImpl) GetIBFTCommittee() map[uint64]*proto.Node {
	return i.ValidatorShare.Committee
}

// GetIdentifier returns ibft identifier made of public key and role (type)
func (i *ibftImpl) GetIdentifier() []byte {
	return i.Identifier //TODO should use mutex to lock var?
}

// resetLeaderSelection resets leader selection with seed and round 1
func (i *ibftImpl) resetLeaderSelection(seed []byte) error {
	return i.leaderSelector.SetSeed(seed, 1)
}

