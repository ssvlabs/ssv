package ibft

import (
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/collections"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

// FirstInstanceIdentifier is the identifier of the first instance in the DB
func FirstInstanceIdentifier() []byte {
	return []byte{0, 0, 0, 0, 0, 0, 0, 0}
}

// StartOptions defines type for IBFT instance options
type StartOptions struct {
	Logger       *zap.Logger
	ValueCheck   valcheck.ValueCheck
	PrevInstance []byte
	Identifier   []byte
	SeqNumber    uint64
	Value        []byte
	Duty         *slotqueue.Duty
}

// IBFT represents behavior of the IBFT
type IBFT interface {
	// StartInstance starts a new instance by the given options
	StartInstance(opts StartOptions) (bool, int, []byte)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() uint64

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[uint64]*proto.Node
}

// ibftImpl implements IBFT interface
type ibftImpl struct {
	instances      []*Instance // key is the instance identifier
	ibftStorage    collections.IbftStorage
	network        network.Network
	msgQueue       *msgqueue.MessageQueue
	params         *proto.InstanceParams
	leaderSelector leader.Selector
}

// New is the constructor of IBFT
func New(storage collections.IbftStorage, network network.Network, queue *msgqueue.MessageQueue, params *proto.InstanceParams) IBFT {
	ret := &ibftImpl{
		ibftStorage:    storage,
		instances:      make([]*Instance, 0),
		network:        network,
		msgQueue:       queue,
		params:         params,
		leaderSelector: &leader.Deterministic{},
	}
	ret.listenToNetworkMessages()
	return ret
}

func (i *ibftImpl) listenToNetworkMessages() {
	msgChan := i.network.ReceivedMsgChan()
	go func() {
		for msg := range msgChan {
			i.msgQueue.AddMessage(&network.Message{
				Lambda:        msg.Message.Lambda,
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
	}()
}

func (i *ibftImpl) StartInstance(opts StartOptions) (bool, int, []byte) {
	instanceOpts := i.instanceOptionsFromStartOptions(opts)

	if err := i.canStartNewInstance(instanceOpts); err != nil {
		opts.Logger.Error("can't start new iBFT instance", zap.Error(err))
	}

	newInstance := NewInstance(instanceOpts)
	//// If previous instance didn't decide, can't start another instance.
	//if !bytes.Equal(opts.PrevInstance, FirstInstanceIdentifier()) {
	//	instance, found := i.instances[hex.EncodeToString(opts.PrevInstance)]
	//	if !found {
	//		opts.Logger.Error("previous instance not found")
	//	}
	//	if instance.Stage() != proto.RoundState_Decided {
	//		opts.Logger.Error("previous instance not decided, can't start new instance")
	//	}
	//}
	//
	//newInstance := NewInstance(InstanceOptions{
	//	Logger: opts.Logger,
	//	Me: &proto.Node{
	//		IbftId: opts.Duty.NodeID,
	//		Pk:     opts.Duty.PrivateKey.GetPublicKey().Serialize(),
	//		Sk:     opts.Duty.PrivateKey.Serialize(),
	//	},
	//	Network:        i.network,
	//	Queue:          i.msgQueue,
	//	ValueCheck:     opts.ValueCheck,
	//	LeaderSelector: i.leaderSelector,
	//	Params: &proto.InstanceParams{
	//		ConsensusParams: i.params.ConsensusParams,
	//		IbftCommittee:   opts.Duty.Committee,
	//	},
	//	Lambda:         opts.Identifier,
	//	PreviousLambda: opts.PrevInstance,
	//})
	i.instances = append(i.instances, newInstance)
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
