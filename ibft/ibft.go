package ibft

import (
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/storage/collections"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
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
	Identifier     []byte
	SeqNumber      uint64
	Value          []byte
	Duty           *ethpb.DutiesResponse_Duty
	ValidatorShare collections.ValidatorShare
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
}

// ibftImpl implements IBFT interface
type ibftImpl struct {
	currentInstance     *Instance
	currentInstanceLock sync.Locker
	logger              *zap.Logger
	ibftStorage         collections.Iibft
	network             network.Network
	msgQueue            *msgqueue.MessageQueue
	params              *proto.InstanceParams // TODO - this should be deprecated for validator share
	ValidatorShare      *collections.ValidatorShare
	leaderSelector      leader.Selector

	// flags
	initFinished bool
}

// New is the constructor of IBFT
func New(
	logger *zap.Logger,
	storage collections.Iibft,
	network network.Network,
	queue *msgqueue.MessageQueue,
	params *proto.InstanceParams,
	ValidatorShare *collections.ValidatorShare,
) IBFT {
	ret := &ibftImpl{
		ibftStorage:         storage,
		currentInstanceLock: &sync.Mutex{},
		logger:              logger,
		network:             network,
		msgQueue:            queue,
		params:              params,
		ValidatorShare:      ValidatorShare,
		leaderSelector:      &leader.Deterministic{},

		// flags
		initFinished: false,
	}
	return ret
}

func (i *ibftImpl) Init() {
	i.listenToSyncMessages()
	i.waitForMinPeerCount(3)
	i.SyncIBFT()
	i.listenToNetworkMessages()
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

	err := i.resetLeaderSelection(opts.Identifier) // Important for deterministic leader selection
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
			if err := i.ibftStorage.SaveCurrentInstance(newInstance.State); err != nil {
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
	return i.params.IbftCommittee
}

// resetLeaderSelection resets leader selection with seed and round 1
func (i *ibftImpl) resetLeaderSelection(seed []byte) error {
	return i.leaderSelector.SetSeed(seed, 1)
}
