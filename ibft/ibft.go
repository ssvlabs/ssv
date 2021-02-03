package ibft

import (
	"encoding/hex"
	"errors"

	"github.com/bloxapp/ssv/storage"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/consensus"
	"github.com/bloxapp/ssv/network"
)

const (
	FirstInstanceIdentifier = ""
)

type StartOptions struct {
	Logger       *zap.Logger
	Consensus    consensus.Consensus
	PrevInstance []byte
	Identifier   []byte
	Value        []byte
	OnCommit     func()
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

func (i *IBFT) StartInstance(opts StartOptions) error {
	prevId := hex.EncodeToString(opts.PrevInstance)
	if prevId != FirstInstanceIdentifier {
		instance, found := i.instances[prevId]
		if !found {
			return errors.New("previous instance not found")
		}
		if instance.Stage() != proto.RoundState_Decided {
			return errors.New("previous instance not decidedChan, can't start new instance")
		}
	}

	newInstance := NewInstance(InstanceOptions{
		Logger:    opts.Logger,
		Me:        i.me,
		Network:   i.network,
		Consensus: opts.Consensus,
		Params:    i.params,
	})
	i.instances[hex.EncodeToString(opts.Identifier)] = newInstance
	newInstance.StartEventLoop()
	newInstance.StartMessagePipeline()
	stageChan := newInstance.GetStageChan()
	go newInstance.Start(opts.PrevInstance, opts.Identifier, opts.Value)

	// Store prepared round and value and decided stage.
	for {
		switch stage := <-stageChan; stage {
		// TODO - complete values
		case proto.RoundState_Prepare:
			i.storage.SavePrepareJustification(opts.Identifier, newInstance.State.Round, nil, nil, []uint64{})
		case proto.RoundState_Commit:
			i.storage.SaveDecidedRound(opts.Identifier, nil, nil, []uint64{})
			return nil
		}
	}

	return nil
}
