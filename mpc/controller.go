package mpc

import (
	"context"
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	controller2 "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/tasks"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
	"sync"
	"time"
)

type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartMpcGroups()
}

type controller struct {
	context    context.Context
	collection validatorstorage.ICollection
	storage    registrystorage.OperatorsCollection
	logger     *zap.Logger
	beacon     beacon.Beacon
	keyManager beacon.KeyManager

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	operatorPubKey             string

	mpcGroupsMap *groupsMap

	metadataUpdateQueue    tasks.Queue
	metadataUpdateInterval time.Duration

	networkMediator controller2.Mediator
	operatorsIDs    *sync.Map
	network         network.Network
}

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler(handlers ...ShareEventHandlerFunc) eth1.SyncEventHandler {
	return func(e eth1.Event) error {
		switch ev := e.Data.(type) {
		case abiparser.ValidatorWithDkgAddedEvent:
		// TODO<MPC>: Add ValidatorWithDkgAddedEvent
		default:
			c.logger.Warn("could not handle unknown event")
		}
		return nil
	}
}

// StartMpcGroups
func (c *controller) StartMpcGroups() {
	// TODO<MPC>: Implement, similar to StartValidators
}

// handleValidatorWithDkgAddedEvent parses the given event and triggers MPC operations
//func (c *controller) handleValidatorWithDkgAddedEvent(event abiparser.ValidatorWithDkgAdded) error {
//	// TODO<MPC>: Implement
//	return errors.New("implement me")
//}
