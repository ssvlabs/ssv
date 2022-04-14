package validator

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/storage"
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/network"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	utilsprotocol "github.com/bloxapp/ssv/protocol/v1/queue"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/prysmaticlabs/prysm/async/event"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	metadataBatchSize = 25
)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *beaconprotocol.Share)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncRateLimit       time.Duration `yaml:"HistorySyncRateLimit" env:"HISTORY_SYNC_BACKOFF" env-default:"200ms" env-description:"Interval for updating metadata"`
	ETHNetwork                 beaconprotocol.Network
	Network                    network.P2PNetwork
	Beacon                     beaconprotocol.Beacon
	Shares                     []ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	CleanRegistryData          bool
	KeyManager                 beaconprotocol.KeyManager
	OperatorPubKey             string
	RegistryStorage            registrystorage.OperatorsCollection
	ForkVersion                forksprotocol.ForkVersion
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartValidators()
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (validator.IValidator, bool)
	UpdateValidatorMetaDataLoop()
	StartNetworkMediators()
	Eth1EventHandler(handlers ...ShareEventHandlerFunc) eth1.SyncEventHandler
	GetAllValidatorShares() ([]*beaconprotocol.Share, error)
}

// controller implements Controller
type controller struct {
	context    context.Context
	collection validator.ICollection
	storage    registrystorage.OperatorsCollection
	logger     *zap.Logger
	beacon     beaconprotocol.Beacon
	keyManager beaconprotocol.KeyManager

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	operatorPubKey             string

	validatorsMap    *validatorsMap
	validatorOptions *validator.Options // TODO(nkryuchkov): check if it's needed

	metadataUpdateQueue    utilsprotocol.Queue
	metadataUpdateInterval time.Duration

	operatorsIDs  *sync.Map
	network       network.P2PNetwork
	forkVersion   forksprotocol.ForkVersion
	messageRouter *messageRouter
	messageWorker *worker.Worker
}

func (c *controller) OnFork(forkVersion forksprotocol.ForkVersion) error {
	c.forkVersion = forkVersion
	return nil
}

// NewController creates a new validator controller instance
func NewController(options ControllerOptions) Controller {
	collection := NewCollection(CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	qbftStorage := storage.New(options.DB, options.Logger, "qbft/")


	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		Logger:       options.Logger,
		WorkersCount: 1,   // TODO flag
		Buffer:       100, // TODO flag
	}

	validatorOptions := &validator.Options{
		Context:                    options.Context,
		Logger:                     options.Logger,
		Network:                    options.ETHNetwork,
		P2pNetwork:                 options.Network,
		Beacon:                     options.Beacon,
		ForkVersion:                options.ForkVersion,
		Signer:                     options.Beacon,
		SyncRateLimit:              options.HistorySyncRateLimit,
		SignatureCollectionTimeout: options.SignatureCollectionTimeout,
		IbftStorage: 				qbftStorage,
		ReadMode:                   false, // set to false for committee validators. if non committee, we set validator with true value
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
		forkVersion:                options.ForkVersion,

		validatorsMap:    newValidatorsMap(options.Context, options.Logger, options.DB, validatorOptions),
		validatorOptions: validatorOptions,

		metadataUpdateQueue:    tasks.NewExecutionQueue(10 * time.Millisecond),
		metadataUpdateInterval: options.MetadataUpdateInterval,

		operatorsIDs: operatorsIDs,

		messageRouter: newMessageRouter(options.Logger),
		messageWorker: worker.NewWorker(workerCfg),
	}

	if err := ctrl.initShares(options); err != nil {
		ctrl.logger.Panic("could not initialize shares", zap.Error(err))
	}
	return &ctrl
}

func (c *controller) GetAllValidatorShares() ([]*beaconprotocol.Share, error) {
	return c.collection.GetAllValidatorShares()
}

func (c *controller) handleRouterMessages() {
	ch := c.messageRouter.GetMessageChan()

	for {
		select {
		case <-c.context.Done():
			return
		case msg := <-ch:
			pk := msg.ID.GetValidatorPK()
			hexPK := hex.EncodeToString(pk)

			if v, ok := c.validatorsMap.GetValidator(hexPK); ok {
				v.ProcessMsg(&msg)
			} else if c.forkVersion != forksprotocol.V0ForkVersion && msg.MsgType == message.SSVPostConsensusMsgType {
				if !c.messageWorker.TryEnqueue(&msg) {
					c.logger.Warn("Failed to enqueue post consensus message: buffer is full")
				}
			}
		}
	}
}

func (c *controller) handleWorkerMessages(msg *message.SSVMessage) error {
	opts := *c.validatorOptions
	opts.ReadMode = true

	val := validator.NewValidator(&opts)
	// TODO(nkryuchkov): we might need to call val.Start(), we need to check it
	val.ProcessMsg(msg) // TODO should return error
	return nil
}

// ListenToEth1Events is listening to events coming from eth1 client
func (c *controller) ListenToEth1Events(feed *event.Feed) {
	cn := make(chan *eth1.Event)
	sub := feed.Subscribe(cn)
	defer sub.Unsubscribe()

	handler := c.Eth1EventHandler(c.handleShare)

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
func (c *controller) Eth1EventHandler(handlers ...ShareEventHandlerFunc) eth1.SyncEventHandler {
	return func(e eth1.Event) error {
		switch ev := e.Data.(type) {
		case abiparser.ValidatorAddedEvent:
			pubKey := hex.EncodeToString(ev.PublicKey)
			// TODO: on history sync this should not be called
			if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
				c.logger.Debug("validator was loaded already")
				return nil
			}
			share, err := c.handleValidatorAddedEvent(ev, e.IsOperatorEvent)
			if err != nil {
				c.logger.Error("could not handle ValidatorAdded event", zap.String("pubkey", pubKey), zap.Error(err))
				return err
			}
			if e.IsOperatorEvent {
				for _, h := range handlers {
					h(share)
				}
			}
		case abiparser.OperatorAddedEvent:
			err := c.handleOperatorAddedEvent(ev)
			if err != nil {
				c.logger.Error("could not handle OperatorAdded event", zap.Error(err))
				return err
			}
		default:
			c.logger.Warn("could not handle unknown event")
		}
		return nil
	}
}

func (c *controller) handleShare(share *beaconprotocol.Share) {
	v := c.validatorsMap.GetOrCreateValidator(share)
	_, err := c.startValidator(v)
	if err != nil {
		c.logger.Warn("could not start validator", zap.Error(err))
	}
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators() {
	shares, err := c.collection.GetOperatorValidatorShares(c.operatorPubKey)
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.setupValidators(shares)
	//// inject handler for finding relevant operators
	//p2p.UseLookupOperatorHandler(c.network, func(oid string) bool {
	//	_, ok := c.operatorsIDs.Load(oid)
	//	return ok
	//})
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
func (c *controller) setupValidators(shares []*beaconprotocol.Share) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var started int
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		v := c.validatorsMap.GetOrCreateValidator(validatorShare)
		pk := v.GetShare().PublicKey.SerializeToHexStr()
		logger := c.logger.With(zap.String("pubkey", pk))
		if !v.GetShare().HasMetadata() { // fetching index and status in case not exist
			fetchMetadata = append(fetchMetadata, v.GetShare().PublicKey.Serialize())
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
	c.network.UseMessageRouter(c.messageRouter)
	go c.handleRouterMessages()
	c.messageWorker.AddHandler(c.handleWorkerMessages)
}

// updateValidatorsMetadata updates metadata of the given public keys.
// as part of the flow in beacon.UpdateValidatorsMetadata,
// UpdateValidatorMetadata is called to persist metadata and start a specific validator
func (c *controller) updateValidatorsMetadata(pubKeys [][]byte) {
	if len(pubKeys) > 0 {
		c.logger.Debug("updating validators", zap.Int("count", len(pubKeys)))
		if err := beaconprotocol.UpdateValidatorsMetadata(pubKeys, c, c.beacon, c.onMetadataUpdated); err != nil {
			c.logger.Error("could not update all validators", zap.Error(err))
		}
	}
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.GetShare().Metadata = metadata
		if err := c.collection.(beaconprotocol.ValidatorMetadataStorage).UpdateValidatorMetadata(pk, metadata); err != nil {
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
func (c *controller) GetValidator(pubKey string) (validator.IValidator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices
// and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices() []spec.ValidatorIndex {
	var toFetch [][]byte
	var indices []spec.ValidatorIndex

	err := c.validatorsMap.ForEach(func(v validator.IValidator) error {
		if !v.GetShare().HasMetadata() {
			toFetch = append(toFetch, v.GetShare().PublicKey.Serialize())
		} else if v.GetShare().Metadata.IsActive() { // eth-client throws error once trying to fetch duties for existed validator
			indices = append(indices, v.GetShare().Metadata.Index)
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
	isOperatorShare bool,
) (*beaconprotocol.Share, error) {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if validator share exist")
	}
	if !found {
		newValShare, shareSecret, err := createShareWithOperatorKey(validatorAddedEvent, c.operatorPubKey, isOperatorShare)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create share")
		}
		if err := c.onNewShare(newValShare, shareSecret); err != nil {
			metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusError))
			return nil, err
		}
		validatorShare = newValShare
		if isOperatorShare {
			logger := c.logger.With(zap.String("pubKey", pubKey))
			logger.Debug("ValidatorAdded event was handled successfully")
		}
	}
	return validatorShare, nil
}

// handleOperatorAddedEvent parses the given event and saves operator information
func (c *controller) handleOperatorAddedEvent(event abiparser.OperatorAddedEvent) error {
	oi := registrystorage.OperatorInformation{
		PublicKey:    string(event.PublicKey),
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
	}
	err := c.storage.SaveOperatorInformation(&oi)
	if err != nil {
		return errors.Wrap(err, "could not save operator information")
	}
	return nil
}

// onMetadataUpdated is called when validator's metadata was updated
func (c *controller) onMetadataUpdated(pk string, meta *beaconprotocol.ValidatorMetadata) {
	if meta == nil {
		return
	}
	if v, exist := c.GetValidator(pk); exist {
		// update share object owned by the validator
		// TODO: check if this updates running validators
		if !v.GetShare().HasMetadata() {
			v.GetShare().Metadata = meta
			c.logger.Debug("metadata was updated", zap.String("pk", pk))
		} else if !v.GetShare().Metadata.Equals(meta) {
			v.GetShare().Metadata.Status = meta.Status
			v.GetShare().Metadata.Balance = meta.Balance
			c.logger.Debug("metadata was updated", zap.String("pk", pk))
		}
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Error("could not start validator after metadata update",
				zap.String("pk", pk), zap.Error(err), zap.Any("metadata", meta))
		}
	}
}

// onNewShare is called when a new validator was added or during registry sync
// if the validator was persisted already, this function won't be called
func (c *controller) onNewShare(share *beaconprotocol.Share, shareSecret *bls.SecretKey) error {
	logger := c.logger.With(zap.String("pubKey", share.PublicKey.SerializeToHexStr()))
	if updated, err := UpdateShareMetadata(share, c.beacon); err != nil {
		logger.Warn("could not add validator metadata", zap.Error(err))
	} else if !updated {
		logger.Warn("could not find validator metadata")
	}

	// in case this validator belongs to operator, the secret key is not nil
	if shareSecret != nil {
		// save secret key
		if err := c.keyManager.AddShare(shareSecret); err != nil {
			return errors.Wrap(err, "failed to save new share secret to key manager")
		}
		logger.Info("share was added successfully to key manager")
	}

	// save validator data
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return errors.Wrap(err, "failed to save new share")
	}
	return nil
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v validator.IValidator) (bool, error) {
	ReportValidatorStatus(v.GetShare().PublicKey.SerializeToHexStr(), v.GetShare().Metadata, c.logger)
	if !v.GetShare().HasMetadata() {
		return false, errors.New("could not start validator: metadata not found")
	}
	if v.GetShare().Metadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	v.Start()
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop() {
	go c.metadataUpdateQueue.Start()

	for {
		time.Sleep(c.metadataUpdateInterval)

		shares, err := c.collection.GetOperatorValidatorShares(c.operatorPubKey)
		if err != nil {
			c.logger.Error("could not get validators shares for metadata update", zap.Error(err))
			continue
		}
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.PublicKey.Serialize())
		}
		c.logger.Debug("updating metadata in loop", zap.Int("shares count", len(shares)))
		beaconprotocol.UpdateValidatorsMetadataBatch(pks, c.metadataUpdateQueue, c,
			c.beacon, c.onMetadataUpdated, metadataBatchSize)
	}
}
