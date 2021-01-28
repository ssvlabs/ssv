package ibft

import (
	"encoding/hex"
	"errors"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/types"
)

const (
	FirstInstanceIdentifier = ""
)

type IBFT struct {
	instances      map[string]*Instance // key is the instance identifier
	db             types.DB
	me             *types.Node
	network        types.Networker
	implementation types.Implementor
	params         *types.InstanceParams
	logger         *zap.Logger
}

func (i *IBFT) StartInstance(prevInstance []byte, identifier, value []byte) error {
	prevId := hex.EncodeToString(prevInstance)
	if prevId != FirstInstanceIdentifier {
		instance, found := i.instances[prevId]
		if !found {
			return errors.New("previous instance not found")
		}
		if instance.Stage() != types.RoundState_Decided {
			return errors.New("previous instance not decided, can't start new instance")
		}
	}

	newInstance := New(i.logger, i.me, i.network, i.implementation, i.params)
	i.instances[hex.EncodeToString(identifier)] = newInstance
	_, err := newInstance.Start(prevInstance, identifier, value)
	return err
}
