package validator

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"sync"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	controller2 "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator/forks"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
)

const (
	metadataBatchSize = 25
)

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

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
	Shares                     []validatorstorage.ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider ShareEncryptionKeyProvider
	CleanRegistryData          bool
	Fork                       forks.Fork
	KeyManager                 beacon.KeyManager
	OperatorPubKey             string
	RegistryStorage            registrystorage.OperatorsCollection
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartValidators()
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (*Validator, bool)
	UpdateValidatorMetaDataLoop()
	StartNetworkMediators()
	Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler
	GetAllValidatorShares() ([]*validatorstorage.Share, error)
}

// controller implements Controller
type controller struct {
	context    context.Context
	collection validatorstorage.ICollection
	storage    registrystorage.OperatorsCollection
	logger     *zap.Logger
	beacon     beacon.Beacon
	keyManager beacon.KeyManager

	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	operatorPubKey             string

	validatorsMap *validatorsMap

	metadataUpdateQueue    tasks.Queue
	metadataUpdateInterval time.Duration

	networkMediator controller2.Mediator
	operatorsIDs    *sync.Map
	network         network.Network
}

// NewController creates a new validator controller instance
func NewController(options ControllerOptions) Controller {
	collection := validatorstorage.NewCollection(validatorstorage.CollectionOptions{
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
		storage:                    options.RegistryStorage,
		context:                    options.Context,
		logger:                     options.Logger.With(zap.String("component", "validatorsController")),
		beacon:                     options.Beacon,
		shareEncryptionKeyProvider: options.ShareEncryptionKeyProvider,
		operatorPubKey:             options.OperatorPubKey,
		keyManager:                 options.KeyManager,
		network:                    options.Network,

		validatorsMap: newValidatorsMap(options.Context, options.Logger, &Options{
			Context:                    options.Context,
			SignatureCollectionTimeout: options.SignatureCollectionTimeout,
			Logger:                     options.Logger,
			Network:                    options.Network,
			ETHNetwork:                 options.ETHNetwork,
			Beacon:                     options.Beacon,
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

	if err := ctrl.initShares(options); err != nil {
		ctrl.logger.Panic("could not initialize shares", zap.Error(err))
	}

	return &ctrl
}

func (c *controller) GetAllValidatorShares() ([]*validatorstorage.Share, error) {
	return c.collection.GetAllValidatorShares()
}

// ListenToEth1Events is listening to events coming from eth1 client
func (c *controller) ListenToEth1Events(feed *event.Feed) {
	cn := make(chan *eth1.Event)
	sub := feed.Subscribe(cn)
	defer sub.Unsubscribe()

	handler := c.Eth1EventHandler(true)

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
func (c *controller) Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler {
	return func(e eth1.Event) error {
		switch e.Name {
		case abiparser.ValidatorAdded:
			ev := e.Data.(abiparser.ValidatorAddedEvent)
			pubKey := hex.EncodeToString(ev.PublicKey)
			if ongoingSync {
				if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
					c.logger.Debug("validator was loaded already")
					return nil
				}
			}
			err := c.handleValidatorAddedEvent(ev, ongoingSync)
			if err != nil {
				c.logger.Error("could not handle ValidatorAdded event", zap.String("pubkey", pubKey), zap.Error(err))
				return err
			}
		case abiparser.ValidatorUpdated:
			ev := e.Data.(abiparser.ValidatorAddedEvent)
			err := c.handleValidatorUpdatedEvent(ev, ongoingSync)
			if err != nil {
				c.logger.Error("could not handle ValidatorUpdated event", zap.Error(err))
				return err
			}
		case abiparser.ValidatorRemoved:
			ev := e.Data.(abiparser.ValidatorRemovedEvent)
			err := c.handleValidatorRemovedEvent(ev, ongoingSync)
			if err != nil {
				c.logger.Error("could not handle ValidatorRemoved event", zap.Error(err))
				return err
			}
		case abiparser.OperatorAdded:
			ev := e.Data.(abiparser.OperatorAddedEvent)
			err := c.handleOperatorAddedEvent(ev)
			if err != nil {
				c.logger.Error("could not handle OperatorAdded event", zap.Error(err))
				return err
			}
		case abiparser.AccountLiquidated:
			ev := e.Data.(abiparser.AccountLiquidatedEvent)
			err := c.handleAccountLiquidatedEvent(ev, ongoingSync)
			if err != nil {
				c.logger.Error("could not handle AccountLiquidated event", zap.Error(err))
				return err
			}
		case abiparser.AccountEnabled:
			ev := e.Data.(abiparser.AccountEnabledEvent)
			err := c.handleAccountEnabledEvent(ev, ongoingSync)
			if err != nil {
				c.logger.Error("could not handle AccountEnabled event", zap.Error(err))
				return err
			}
		default:
			c.logger.Warn("could not handle unknown event")
		}
		return nil
	}
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators() {
	shares, err := c.collection.GetEnabledOperatorValidatorShares(c.operatorPubKey)
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.setupValidators(shares)
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
}

// setupValidators setup and starts validators from the given shares
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (c *controller) setupValidators(shares []*validatorstorage.Share) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var started int
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		v := c.validatorsMap.GetOrCreateValidator(validatorShare)
		pk := v.Share.PublicKey.SerializeToHexStr()
		logger := c.logger.With(zap.String("pubkey", pk))
		if !v.Share.HasMetadata() { // fetching index and status in case not exist
			fetchMetadata = append(fetchMetadata, v.Share.PublicKey.Serialize())
			logger.Warn("could not start validator as metadata not found")
			continue
		}
		isStarted, err := c.startValidator(v)
		if err != nil {
			logger.Warn("could not start validator", zap.Error(err))
			errs = append(errs, err)
		}
		if isStarted {
			started++
		}
	}
	c.logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.Size()),
		zap.Int("failures", len(errs)), zap.Int("missing metadata", len(fetchMetadata)),
		zap.Int("shares count", len(shares)), zap.Int("started", started))

	go c.updateValidatorsMetadata(fetchMetadata)
}

func (c *controller) StartNetworkMediators() {
	msgChan, msgDone := c.validatorsMap.optsTemplate.Network.ReceivedMsgChan()
	decidedChan, decidedDone := c.validatorsMap.optsTemplate.Network.ReceivedDecidedChan()

	c.networkMediator.AddListener(network.NetworkMsg_IBFTType, msgChan, msgDone, c.getReader)
	c.networkMediator.AddListener(network.NetworkMsg_DecidedType, decidedChan, decidedDone, c.getReader)
}

func (c *controller) getReader(publicKey string) (controller2.MediatorReader, bool) {
	return c.validatorsMap.GetValidator(publicKey)
}

// updateValidatorsMetadata updates metadata of the given public keys.
// as part of the flow in beacon.UpdateValidatorsMetadata,
// UpdateValidatorMetadata is called to persist metadata and start a specific validator
func (c *controller) updateValidatorsMetadata(pubKeys [][]byte) {
	if len(pubKeys) > 0 {
		c.logger.Debug("updating validators", zap.Int("count", len(pubKeys)))
		if err := beacon.UpdateValidatorsMetadata(pubKeys, c, c.beacon, c.onMetadataUpdated); err != nil {
			c.logger.Error("could not update all validators", zap.Error(err))
		}
	}
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(pk string, metadata *beacon.ValidatorMetadata) error {
	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.Share.Metadata = metadata
		if err := c.collection.(beacon.ValidatorMetadataStorage).UpdateValidatorMetadata(pk, metadata); err != nil {
			return err
		}
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Error("could not start validator", zap.Error(err))
		}
	}
	return nil
}

// GetValidator returns a validator instance from validatorsMap
func (c *controller) GetValidator(pubKey string) (*Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices
// and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices() []spec.ValidatorIndex {
	var toFetch [][]byte
	var indices []spec.ValidatorIndex

	err := c.validatorsMap.ForEach(func(v *Validator) error {
		if !v.Share.HasMetadata() {
			toFetch = append(toFetch, v.Share.PublicKey.Serialize())
		} else if v.Share.Metadata.IsActive() { // eth-client throws error once trying to fetch duties for existed validator
			indices = append(indices, v.Share.Metadata.Index)
		}
		return nil
	})
	if err != nil {
		c.logger.Error("failed to get all validators public keys", zap.Error(err))
	}

	go c.updateValidatorsMetadata(toFetch)

	return indices
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(
	validatorAddedEvent abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) error {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		validatorShare, _, err = c.onShareCreate(validatorAddedEvent)
		if err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return err
		}
	}

	isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
	if isOperatorShare {
		logger := c.logger.With(zap.String("pubKey", pubKey))
		logger.Debug("ValidatorAdded event was handled successfully")
		metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
		if ongoingSync {
			c.onShareStart(validatorShare)
		}
	}
	return nil
}

// handleValidatorUpdatedEvent handles registry contract event for validator updated
func (c *controller) handleValidatorUpdatedEvent(
	validatorUpdatedEvent abiparser.ValidatorAddedEvent,
	ongoingSync bool,
) error {
	validatorShare, found, err := c.collection.GetValidatorShare(validatorUpdatedEvent.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return errors.New("could not find validator share")
	}

	validatorUpdated, isOperatorEvent, err := c.onShareCreate(validatorUpdatedEvent)
	if err != nil {
		return err
	}

	if !ongoingSync {
		return nil
	}

	// determine if validator share belongs to operator
	isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)

	// not mine
	if !isOperatorShare && !isOperatorEvent {
		// do nothing
	}

	// stay mine
	if isOperatorShare && isOperatorEvent {
		if err := c.onShareRemove(validatorShare.PublicKey.SerializeToHexStr(), false); err != nil {
			return err
		}
		// TODO: validate removed from map, not running

		// TODO: (wait few epochs logic)
		c.onShareStart(validatorUpdated)
	}

	// was mine
	if isOperatorShare && !isOperatorEvent {
		if err := c.onShareRemove(validatorShare.PublicKey.SerializeToHexStr(), false); err != nil {
			return err
		}
	}

	// became mine
	if !isOperatorShare && isOperatorEvent {
		// TODO: validate the validator is not attesting (eth2)

		// TODO: (wait few epochs logic)
		c.onShareStart(validatorUpdated)
	}

	return nil
}

// handleValidatorRemovedEvent handles registry contract event for validator removed
func (c *controller) handleValidatorRemovedEvent(
	validatorRemovedEvent abiparser.ValidatorRemovedEvent,
	ongoingSync bool,
) error {
	// TODO: handle metrics
	validatorShare, found, err := c.collection.GetValidatorShare(validatorRemovedEvent.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		return errors.New("could not find validator share")
	}

	// remove from storage
	if err := c.collection.DeleteValidatorShare(validatorShare.PublicKey.Serialize()); err != nil {
		return errors.Wrap(err, "could not remove validator share")
	}

	if ongoingSync {
		// determine if validator share belongs to operator
		isOperatorShare := validatorShare.IsOperatorShare(c.operatorPubKey)
		if isOperatorShare {
			if err := c.onShareRemove(validatorShare.PublicKey.SerializeToHexStr(), true); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleOperatorAddedEvent parses the given event and saves operator information
func (c *controller) handleOperatorAddedEvent(event abiparser.OperatorAddedEvent) error {
	eventOperatorPubKey := string(event.PublicKey)
	od := registrystorage.OperatorData{
		PublicKey:    eventOperatorPubKey,
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
		Index:        event.Id.Uint64(),
	}
	if err := c.storage.SaveOperatorData(&od); err != nil {
		return errors.Wrap(err, "could not save operator data")
	}

	return nil
}

// handleAccountLiquidatedEvent handles registry contract event for account liquidated
func (c *controller) handleAccountLiquidatedEvent(
	event abiparser.AccountLiquidatedEvent,
	ongoingSync bool,
) error {
	ownerAddress := event.OwnerAddress.String()
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get validator shares by owner address")
	}

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			share.Liquidated = true

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				// we can't remove the share secret from key-manager
				// due to the fact that after activating the validators (AccountEnabled)
				// we don't have the encrypted keys to decrypt the secret, but only the owner address
				if err := c.onShareRemove(share.PublicKey.SerializeToHexStr(), false); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// handle AccountEnabledEvent handles registry contract event for account enabled
func (c *controller) handleAccountEnabledEvent(
	event abiparser.AccountEnabledEvent,
	ongoingSync bool,
) error {
	ownerAddress := event.OwnerAddress.String()
	shares, err := c.collection.GetValidatorSharesByOwnerAddress(ownerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get validator shares by owner address")
	}

	for _, share := range shares {
		if share.IsOperatorShare(c.operatorPubKey) {
			share.Liquidated = false

			// save validator data
			if err := c.collection.SaveValidatorShare(share); err != nil {
				return errors.Wrap(err, "could not save validator share")
			}

			if ongoingSync {
				c.onShareStart(share)
			}
		}
	}

	return nil
}

// onMetadataUpdated is called when validator's metadata was updated
func (c *controller) onMetadataUpdated(pk string, meta *beacon.ValidatorMetadata) {
	if meta == nil {
		return
	}
	if v, exist := c.GetValidator(pk); exist {
		// update share object owned by the validator
		// TODO: check if this updates running validators
		if !v.Share.HasMetadata() {
			v.Share.Metadata = meta
			c.logger.Debug("metadata was updated", zap.String("pk", pk))
		} else if !v.Share.Metadata.Equals(meta) {
			v.Share.Metadata.Status = meta.Status
			v.Share.Metadata.Balance = meta.Balance
			c.logger.Debug("metadata was updated", zap.String("pk", pk))
		}
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Error("could not start validator after metadata update",
				zap.String("pk", pk), zap.Error(err), zap.Any("metadata", meta))
		}
	}
}

// onShareCreate is called when a validator was added/updated during registry sync
func (c *controller) onShareCreate(validatorEvent abiparser.ValidatorAddedEvent) (*validatorstorage.Share, bool, error) {
	share, shareSecret, err := ShareFromValidatorEvent(
		validatorEvent,
		c.storage.GetOperatorData,
		c.shareEncryptionKeyProvider,
		c.operatorPubKey,
	)
	if err != nil {
		return nil, false, errors.Wrap(err, "could not extract validator share from event")
	}

	// determine if the share belongs to operator
	isOperatorShare := share.IsOperatorShare(c.operatorPubKey)

	if isOperatorShare {
		if shareSecret == nil {
			return nil, isOperatorShare, errors.New("could not decode shareSecret key from ValidatorAdded event")
		}

		logger := c.logger.With(zap.String("pubKey", share.PublicKey.SerializeToHexStr()))

		// get metadata
		if updated, err := UpdateShareMetadata(share, c.beacon); err != nil {
			logger.Warn("could not add validator metadata", zap.Error(err))
		} else if !updated {
			logger.Warn("could not find validator metadata")
		}

		// save secret key
		if err := c.keyManager.AddShare(shareSecret); err != nil {
			return nil, isOperatorShare, errors.Wrap(err, "could not add share secret to key manager")
		}
		logger.Info("share was added successfully to key manager")
	}

	// save validator data
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return nil, isOperatorShare, errors.Wrap(err, "could not save validator share")
	}

	return share, isOperatorShare, nil
}

// onShareRemove is called when a validator was removed
func (c *controller) onShareRemove(pk string, removeSecret bool) error {
	// remove from validatorsMap
	v := c.validatorsMap.RemoveValidator(pk)

	// stop instance
	if v != nil {
		if err := v.Close(); err == nil {
			return errors.Wrap(err, "could not close validator")
		}
	}

	// remove the share secret from key-manager
	if removeSecret {
		if err := c.keyManager.RemoveShare(pk); err != nil {
			return errors.Wrap(err, "could not remove share secret from key manager")
		}
	}

	return nil
}

func (c *controller) onShareStart(share *validatorstorage.Share) {
	v := c.validatorsMap.GetOrCreateValidator(share)
	_, err := c.startValidator(v)
	if err != nil {
		c.logger.Warn("could not start validator", zap.Error(err))
	}
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *Validator) (bool, error) {
	ReportValidatorStatus(v.Share.PublicKey.SerializeToHexStr(), v.Share.Metadata, c.logger)
	if !v.Share.HasMetadata() {
		return false, errors.New("could not start validator: metadata not found")
	}
	if v.Share.Metadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	if err := v.Start(); err != nil {
		metricsValidatorStatus.WithLabelValues(v.Share.PublicKey.SerializeToHexStr()).Set(float64(validatorStatusError))
		return false, errors.Wrap(err, "could not start validator")
	}
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop() {
	go c.metadataUpdateQueue.Start()

	for {
		time.Sleep(c.metadataUpdateInterval)

		shares, err := c.collection.GetEnabledOperatorValidatorShares(c.operatorPubKey)
		if err != nil {
			c.logger.Error("could not get validators shares for metadata update", zap.Error(err))
			continue
		}
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.PublicKey.Serialize())
		}
		c.logger.Debug("updating metadata in loop", zap.Int("shares count", len(shares)))
		beacon.UpdateValidatorsMetadataBatch(pks, c.metadataUpdateQueue, c,
			c.beacon, c.onMetadataUpdated, metadataBatchSize)
	}
}
