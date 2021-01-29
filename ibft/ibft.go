package ibft

import (
	"encoding/hex"
	"errors"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/consensus"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
)

const (
	FirstInstanceIdentifier = ""
)

type IBFT struct {
	instances map[string]*Instance // key is the instance identifier
	storage   storage.Storage
	me        *proto.Node
	network   network.Network
	consensus consensus.Consensus
	params    *proto.InstanceParams
}

// New is the constructor of IBFT
func New(storage storage.Storage, me *proto.Node, network network.Network, consensus consensus.Consensus, params *proto.InstanceParams) *IBFT {
	return &IBFT{
		instances: make(map[string]*Instance),
		storage:   storage,
		me:        me,
		network:   network,
		consensus: consensus,
		params:    params,
	}
}

func (i *IBFT) StartInstance(logger *zap.Logger, prevInstance []byte, identifier, value []byte) error {
	prevId := hex.EncodeToString(prevInstance)
	if prevId != FirstInstanceIdentifier {
		instance, found := i.instances[prevId]
		if !found {
			return errors.New("previous instance not found")
		}
		if instance.Stage() != proto.RoundState_Decided {
			return errors.New("previous instance not decided, can't start new instance")
		}
	}

	newInstance := NewInstance(InstanceOptions{
		Logger:    logger,
		Me:        i.me,
		Network:   i.network,
		Consensus: i.consensus,
		Params:    i.params,
	})
	i.instances[hex.EncodeToString(identifier)] = newInstance
	newInstance.StartEventLoopAndMessagePipeline()
	_, err := newInstance.Start(prevInstance, identifier, value)
	return err
}
