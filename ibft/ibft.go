package ibft

import (
	"encoding/hex"
	"errors"

	"github.com/bloxapp/ssv/storage"

	"github.com/bloxapp/ssv/ibft/proto"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/valueImpl"
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

func (i *IBFT) StartInstance(logger *zap.Logger, consensus valueImpl.ValueImplementation, prevInstance []byte, identifier, value []byte) error {
	prevId := hex.EncodeToString(prevInstance)
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
		Logger:    logger,
		Me:        i.me,
		Network:   i.network,
		Consensus: consensus,
		Params:    i.params,
	})
	i.instances[hex.EncodeToString(identifier)] = newInstance
	newInstance.StartEventLoop()
	newInstance.StartMessagePipeline()
	stageChan := newInstance.GetStageChan()
	go newInstance.Start(prevInstance, identifier, value)

	// Store prepared round and value and decided stage.
	go func() {
		for {
			select {
			case stage := <-stageChan:
				switch stage {
				// TODO - complete values
				case proto.RoundState_Prepare:
					i.storage.SavePrepareJustification(identifier, newInstance.State.Round, nil, nil, []uint64{})
				case proto.RoundState_Decided:
					i.storage.SaveDecidedRound(identifier, nil, nil, []uint64{})
					// reconstruct sig
					//sigs, ids := i.instances[hex.EncodeToString(identifier)].SignedValues()
					// TODO - reconstruct
					return
				}
			}
		}
	}()

	return nil
}
