package ibft

import (
	"encoding/hex"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/utils/dataval"
)

const (
	FirstInstanceIdentifier = ""
)

type StartOptions struct {
	Logger       *zap.Logger
	Consensus    dataval.Validator
	PrevInstance []byte
	Identifier   []byte
	Value        []byte
}

type IBFT struct {
	instances map[string]*Instance // key is the instance identifier
	storage   storage.Storage
	me        *proto.Node
	network   network.Network
	params    *proto.InstanceParams
}

// New is the constructor of IBFT
func New(storage storage.Storage, me *proto.Node, network network.Network, params *proto.InstanceParams) *IBFT {
	return &IBFT{
		instances: make(map[string]*Instance),
		storage:   storage,
		me:        me,
		network:   network,
		params:    params,
	}
}

func (i *IBFT) StartInstance(opts StartOptions) (bool, int) {
	// If previous instance didn't decide, can't start another instance.
	prevId := hex.EncodeToString(opts.PrevInstance)
	if prevId != FirstInstanceIdentifier {
		instance, found := i.instances[prevId]
		if !found {
			opts.Logger.Fatal("previous instance not found")
		}
		if instance.Stage() != proto.RoundState_Decided {
			opts.Logger.Fatal("previous instance not decided, can't start new instance")
		}
	}

	newInstance := NewInstance(InstanceOptions{
		Logger:         opts.Logger,
		Me:             i.me,
		Network:        i.network,
		Consensus:      opts.Consensus,
		Params:         i.params,
		Lambda:         opts.Identifier,
		PreviousLambda: opts.PrevInstance,
	})
	i.instances[hex.EncodeToString(opts.Identifier)] = newInstance
	newInstance.StartEventLoop()
	newInstance.StartMessagePipeline()
	stageChan := newInstance.GetStageChan()
	go newInstance.Start(opts.Value)

	// Store prepared round and value and decided stage.
	for {
		switch stage := <-stageChan; stage {
		// TODO - complete values
		case proto.RoundState_Prepare:
			agg, err := newInstance.PreparedAggregatedMsg()
			if err != nil {
				newInstance.logger.Fatal("could not get aggregated prepare msg and save to storage", zap.Error(err))
				return false, 0
			}
			i.storage.SavePrepared(agg)
		case proto.RoundState_Commit:
			agg, err := newInstance.CommittedAggregatedMsg()
			if err != nil {
				newInstance.logger.Fatal("could not get aggregated commit msg and save to storage", zap.Error(err))
				return false, 0
			}
			i.storage.SaveDecided(agg)
		case proto.RoundState_Decided:
			agg, err := newInstance.CommittedAggregatedMsg()
			if err != nil {
				newInstance.logger.Fatal("could not get aggregated commit msg and save to storage", zap.Error(err))
				return false, 0
			}

			return true, len(agg.GetSignerIds())
		}
	}
}
