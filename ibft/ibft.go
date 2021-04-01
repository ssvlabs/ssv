package ibft

import (
	"bytes"
	"encoding/hex"

	"github.com/bloxapp/ssv/ibft/leader"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/utils/dataval"
)

// FirstInstanceIdentifier is the identifier of the first instance in the DB
func FirstInstanceIdentifier() []byte {
	return []byte{0, 0, 0, 0, 0, 0, 0, 0}
}

// StartOptions defines type for IBFT instance options
type StartOptions struct {
	Logger       *zap.Logger
	Consensus    dataval.Validator
	PrevInstance []byte
	Identifier   []byte
	Value        []byte
}

// IBFT represents behavior of the IBFT
type IBFT interface {
	// StartInstance starts a new instance by the given options
	StartInstance(opts StartOptions) (bool, int)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[uint64]*proto.Node
}

// ibftImpl implements IBFT interface
type ibftImpl struct {
	instances      map[string]*Instance // key is the instance identifier
	storage        storage.Storage
	me             *proto.Node
	network        network.Network
	params         *proto.InstanceParams
	leaderSelector leader.Selector
}

// New is the constructor of IBFT
func New(storage storage.Storage, me *proto.Node, network network.Network, params *proto.InstanceParams) IBFT {
	return &ibftImpl{
		instances:      make(map[string]*Instance),
		storage:        storage,
		me:             me,
		network:        network,
		params:         params,
		leaderSelector: &leader.Deterministic{},
	}
}

func (i *ibftImpl) StartInstance(opts StartOptions) (bool, int) {
	// If previous instance didn't decide, can't start another instance.
	if !bytes.Equal(opts.PrevInstance, FirstInstanceIdentifier()) {
		instance, found := i.instances[hex.EncodeToString(opts.PrevInstance)]
		if !found {
			opts.Logger.Error("previous instance not found")
		}
		if instance.Stage() != proto.RoundState_Decided {
			opts.Logger.Error("previous instance not decided, can't start new instance")
		}
	}

	newInstance := NewInstance(InstanceOptions{
		Logger:         opts.Logger,
		Me:             i.me,
		Network:        i.network,
		Consensus:      opts.Consensus,
		LeaderSelector: i.leaderSelector,
		Params:         i.params,
		Lambda:         opts.Identifier,
		PreviousLambda: opts.PrevInstance,
	})
	i.instances[hex.EncodeToString(opts.Identifier)] = newInstance
	newInstance.StartEventLoop()
	newInstance.StartMessagePipeline()
	stageChan := newInstance.GetStageChan()
	i.resetLeaderSelection(opts.Identifier) // Important for deterministic leader selection
	go newInstance.Start(opts.Value)

	// Store prepared round and value and decided stage.
	for {
		switch stage := <-stageChan; stage {
		// TODO - complete values
		case proto.RoundState_Prepare:
			agg, err := newInstance.PreparedAggregatedMsg()
			if err != nil {
				newInstance.Logger.Fatal("could not get aggregated prepare msg and save to storage", zap.Error(err))
				return false, 0
			}
			i.storage.SavePrepared(agg)
		case proto.RoundState_Decided:
			agg, err := newInstance.CommittedAggregatedMsg()
			if err != nil {
				newInstance.Logger.Fatal("could not get aggregated commit msg and save to storage", zap.Error(err))
				return false, 0
			}
			i.storage.SaveDecided(agg)
			return true, len(agg.GetSignerIds())
		}
	}
}

// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
func (i *ibftImpl) GetIBFTCommittee() map[uint64]*proto.Node {
	return i.params.IbftCommittee
}

// resetLeaderSelection resets leader selection with seed and round 1
func (i *ibftImpl) resetLeaderSelection(seed []byte) {
	i.leaderSelector.SetSeed(seed, 1)
}
