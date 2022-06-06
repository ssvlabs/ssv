package mpc

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	controller2 "github.com/bloxapp/ssv/ibft/controller"
	mpcstorage "github.com/bloxapp/ssv/mpc/storage"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/forks"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"

	//validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
	"sync"
	"time"
)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncRateLimit       time.Duration `yaml:"HistorySyncRateLimit" env:"HISTORY_SYNC_BACKOFF" env-default:"200ms" env-description:"Interval for updating metadata"`
	ETHNetwork                 *core.Network
	Network                    network.Network
	Beacon                     beacon.Beacon
	//Shares                     []validatorstorage.ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	CleanRegistryData          bool
	Fork                       forks.Fork
	KeyManager                 beacon.KeyManager
	OperatorPubKey             string
	RegistryStorage            registrystorage.OperatorsCollection
}

type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartMpcGroups()
}

type controller struct {
	context             context.Context
	collection          mpcstorage.ICollection
	operatorsCollection registrystorage.OperatorsCollection
	logger              *zap.Logger
	beacon              beacon.Beacon
	keyManager          beacon.KeyManager

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	operatorPubKey             string

	groupsMap *groupsMap

	metadataUpdateQueue    tasks.Queue
	metadataUpdateInterval time.Duration

	networkMediator controller2.Mediator
	operatorsIDs    *sync.Map
	network         network.Network
}

// NewController creates a new validator controller instance
func NewController(options ControllerOptions) Controller {
	collection := mpcstorage.NewCollection(mpcstorage.CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}
	notifyOperatorID := func(oid string) {
		operatorsIDs.Store(oid, true)
		// TODO: update network in a better way
		options.Network.NotifyOperatorID(oid)
	}

	ctrl := controller{
		collection:                 collection,
		operatorsCollection:        options.RegistryStorage,
		context:                    options.Context,
		logger:                     options.Logger.With(zap.String("component", "validatorsController")),
		beacon:                     options.Beacon,
		shareEncryptionKeyProvider: options.ShareEncryptionKeyProvider,
		operatorPubKey:             options.OperatorPubKey,
		keyManager:                 options.KeyManager,
		network:                    options.Network,
		groupsMap: newGroupsMap(options.Context, options.Logger, &Options{
			Context:                    options.Context,
			SignatureCollectionTimeout: options.SignatureCollectionTimeout,
			Logger:                     options.Logger,
			Network:                    options.Network,
			ETHNetwork:                 options.ETHNetwork,
			DB:                         options.DB,
			Fork:                       options.Fork,
			Signer:                     options.KeyManager,
			SyncRateLimit:              options.HistorySyncRateLimit,
			notifyOperatorID:           notifyOperatorID,
		}),

		metadataUpdateQueue:    tasks.NewExecutionQueue(10 * time.Millisecond),
		metadataUpdateInterval: options.MetadataUpdateInterval,

		networkMediator: controller2.NewMediator(options.Logger),
		operatorsIDs:    operatorsIDs,
	}

	return &ctrl
}

// ListenToEth1Events is listening to events coming from eth1 client
func (c *controller) ListenToEth1Events(feed *event.Feed) {
	cn := make(chan *eth1.Event)
	sub := feed.Subscribe(cn)
	defer sub.Unsubscribe()

	handler := c.Eth1EventHandler()

	for {
		select {
		case e := <-cn:
			if err := handler(*e); err != nil {
				c.logger.Error("could not process ongoing eth1 event", zap.Error(err))
			}
		case err := <-sub.Err():
			c.logger.Error("event feed subscription error", zap.Error(err))
		}
	}
}

// Eth1EventHandler is a factory function for creating eth1 event handler
func (c *controller) Eth1EventHandler() eth1.SyncEventHandler {
	return func(e eth1.Event) error {
		switch ev := e.Data.(type) {
		case abiparser.DistributedKeyRequestedEvent:
			if _, ok := c.groupsMap.GetMpcGroup(ev.RequestId); ok {
				c.logger.Debug("validator was loaded already")
				return nil
			}
			err := c.handleDistributedKeyRequestedEvent(ev)
			if err != nil {
				c.logger.Error("could not handle DistributedKeyRequested event", zap.String("requestId", ev.RequestId.String()), zap.Error(err))
				return err
			}
			c.logger.Debug("received", zap.Any("event", ev))
		default:
			c.logger.Warn("could not handle unknown event")
		}
		return nil
	}
}

func (c *controller) StartMpcGroups() {
	// TODO<MPC>: Implement, similar to StartValidators
	requests, err := c.collection.ListDkgRequests()
	if err != nil {
		c.logger.Fatal("failed to get DKG requests", zap.Error(err))
	}
	if len(requests) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.processMissedRequests(requests)
	/**
	// inject handler for finding relevant operators
	p2p.UseLookupOperatorHandler(c.network, func(oid string) bool {
		_, ok := c.operatorsIDs.Load(oid)
		return ok
	})
	// print current relevant operators (ids)
	ids := []string{}
	c.operatorsIDs.Range(func(key, value interface{}) bool {
		ids = append(ids, key.(string))
		return true
	})
	c.logger.Debug("relevant operators", zap.Int("len", len(ids)), zap.Strings("op_ids", ids))
	*/
}

func (c *controller) processMissedRequests(requests []mpcstorage.DkgRequest) {

}

//handleValidatorWithDkgAddedEvent parses the given event and triggers MPC operations
func (c *controller) handleDistributedKeyRequestedEvent(event abiparser.DistributedKeyRequestedEvent) error {
	request, err := createRequest(event, c.operatorPubKey)
	if err != nil {
		return errors.Wrap(err, "failed to create dkg request")
	}
	c.handleRequest(request)
	if err != nil {
		return err
	}
	return nil
}

func (c *controller) handleRequest(request *mpcstorage.DkgRequest) {
	v := c.groupsMap.GetOrCreateMpcGroup(request)
	_, err := c.startGroup(v)
	if err != nil {
		c.logger.Warn("could not start validator", zap.Error(err))
	}
}

func (c *controller) startGroup(g *Group) (bool, error) {
	return true, nil
}
