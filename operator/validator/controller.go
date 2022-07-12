package validator

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"sync"
	"time"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	utilsprotocol "github.com/bloxapp/ssv/protocol/v1/queue"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"github.com/bloxapp/ssv/protocol/v1/sync/handlers"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	metadataBatchSize = 25
)

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

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
	ShareEncryptionKeyProvider ShareEncryptionKeyProvider
	CleanRegistryData          bool
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Flag that indicates whether the node saves decided history or just the latest messages"`
	KeyManager                 beaconprotocol.KeyManager
	OperatorPubKey             string
	RegistryStorage            registrystorage.OperatorsCollection
	ForkVersion                forksprotocol.ForkVersion
	NewDecidedHandler          qbftcontroller.NewDecidedHandler

	// worker flags
	WorkersCount    int `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"512" env-description:"Number of goroutines to use for message workers"`
	QueueBufferSize int `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"1024" env-description:"Buffer size for message workers"`
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartValidators()
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (validator.IValidator, bool)
	UpdateValidatorMetaDataLoop()
	StartNetworkHandlers()
	Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler
	GetAllValidatorShares() ([]*beaconprotocol.Share, error)
	OnFork(forkVersion forksprotocol.ForkVersion) error
}

// controller implements Controller
type controller struct {
	context    context.Context
	collection validator.ICollection
	storage    registrystorage.OperatorsCollection
	logger     *zap.Logger
	beacon     beaconprotocol.Beacon
	keyManager beaconprotocol.KeyManager

	shareEncryptionKeyProvider ShareEncryptionKeyProvider
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

// OnFork called upon a fork, it will propagate the fork event to all internal components.
// triggering validators fork with goroutines as validator.OnFork might block due to
// decided message processing in the qbft controllers
func (c *controller) OnFork(forkVersion forksprotocol.ForkVersion) error {
	c.forkVersion = forkVersion
	c.validatorOptions.ForkVersion = forkVersion

	storageHandler, ok := c.validatorOptions.IbftStorage.(forksprotocol.ForkHandler)
	if !ok {
		return errors.New("ibft storage is not a fork handler")
	}
	err := storageHandler.OnFork(forkVersion)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var errLock sync.Mutex
	_ = c.validatorsMap.ForEach(func(iValidator validator.IValidator) error {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if localErr := iValidator.OnFork(forkVersion); localErr != nil {
				errLock.Lock()
				err = localErr
				errLock.Unlock()
			}
		}()
		return nil
	})
	wg.Wait()

	return err
}

// NewController creates a new validator controller instance
func NewController(options ControllerOptions) Controller {
	collection := NewCollection(CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	qbftStorage := storage.New(options.DB, options.Logger, message.RoleTypeAttester.String(), options.ForkVersion) // TODO need to support multi duties

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		Logger:       options.Logger,
		WorkersCount: options.WorkersCount,
		Buffer:       options.QueueBufferSize,
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
		IbftStorage:                qbftStorage,
		ReadMode:                   false, // set to false for committee validators. if non committee, we set validator with true value
		FullNode:                   options.FullNode,
		NewDecidedHandler:          options.NewDecidedHandler,
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

	if err := ctrl.setupNetworkHandlers(); err != nil {
		ctrl.logger.Panic("could not initialize shares", zap.Error(err))
	}

	return &ctrl
}

// setupNetworkHandlers registers all the required handlers for sync protocols
func (c *controller) setupNetworkHandlers() error {
	c.network.RegisterHandlers(p2pprotocol.WithHandler(
		p2pprotocol.LastDecidedProtocol,
		handlers.LastDecidedHandler(c.logger, c.validatorOptions.IbftStorage, c.network),
	), p2pprotocol.WithHandler(
		p2pprotocol.LastChangeRoundProtocol,
		handlers.LastChangeRoundHandler(c.logger, c.validatorOptions.IbftStorage, c.network),
	), p2pprotocol.WithHandler(
		p2pprotocol.DecidedHistoryProtocol,
		// TODO: extract maxBatch to config
		handlers.HistoryHandler(c.logger, c.validatorOptions.IbftStorage, c.network, 25),
	))
	return nil
}

func (c *controller) GetAllValidatorShares() ([]*beaconprotocol.Share, error) {
	return c.collection.GetAllValidatorShares()
}

func (c *controller) handleRouterMessages() {
	ctx, cancel := context.WithCancel(c.context)
	defer cancel()
	ch := c.messageRouter.GetMessageChan()

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("router message handler stopped")
			return
		case msg := <-ch:
			pk := msg.ID.GetValidatorPK()
			hexPK := hex.EncodeToString(pk)

			if v, ok := c.validatorsMap.GetValidator(hexPK); ok {
				if err := v.ProcessMsg(&msg); err != nil {
					c.logger.Warn("failed to process message", zap.Error(err))
				}
			} else if c.forkVersion != forksprotocol.V0ForkVersion {
				if msg.MsgType != message.SSVDecidedMsgType && msg.MsgType != message.SSVConsensusMsgType {
					continue // not supporting other types
				}
				if !c.messageWorker.TryEnqueue(&msg) { // start to save non committee decided messages only post fork
					c.logger.Warn("Failed to enqueue post consensus message: buffer is full")
				}
			}
		}
	}
}

// getShare returns the share of the given validator public key
// TODO: optimize
func (c *controller) getShare(pk message.ValidatorPK) (*beaconprotocol.Share, error) {
	share, found, err := c.collection.GetValidatorShare(pk)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read validator share [%s]", pk)
	}
	if !found {
		return nil, nil
	}
	return share, nil
}

func (c *controller) handleWorkerMessages(msg *message.SSVMessage) error {
	share, err := c.getShare(msg.GetIdentifier().GetValidatorPK())
	if err != nil {
		return err
	}
	if share == nil {
		return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetIdentifier().GetValidatorPK()))
	}

	opts := *c.validatorOptions
	opts.Share = share
	opts.ReadMode = true

	return validator.NewValidator(&opts).ProcessMsg(msg)
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
			logFields, err := handler(*e)
			_ = eth1.HandleEventResult(c.logger, *e, logFields, err, true)
		case err := <-sub.Err():
			c.logger.Warn("event feed subscription error", zap.Error(err))
		}
	}
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators() {
	shares, err := c.collection.GetOperatorValidatorShares(c.operatorPubKey, true)
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.setupValidators(shares)
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

// StartNetworkHandlers init msg worker that handles network messages
func (c *controller) StartNetworkHandlers() {
	c.network.UseMessageRouter(c.messageRouter)
	go c.handleRouterMessages()
	c.messageWorker.UseHandler(c.handleWorkerMessages)
}

// updateValidatorsMetadata updates metadata of the given public keys.
// as part of the flow in beacon.UpdateValidatorsMetadata,
// UpdateValidatorMetadata is called to persist metadata and start a specific validator
func (c *controller) updateValidatorsMetadata(pubKeys [][]byte) {
	if len(pubKeys) > 0 {
		c.logger.Debug("updating validators", zap.Int("count", len(pubKeys)))
		if err := beaconprotocol.UpdateValidatorsMetadata(pubKeys, c, c.beacon, c.onMetadataUpdated); err != nil {
			c.logger.Warn("could not update all validators", zap.Error(err))
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
			c.logger.Warn("could not start validator", zap.Error(err))
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
		c.logger.Warn("failed to get all validators public keys", zap.Error(err))
	}

	go c.updateValidatorsMetadata(toFetch)

	return indices
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
			c.logger.Warn("could not start validator after metadata update",
				zap.String("pk", pk), zap.Error(err), zap.Any("metadata", meta))
		}
	}
}

// onShareCreate is called when a validator was added/updated during registry sync
func (c *controller) onShareCreate(validatorEvent abiparser.ValidatorRegistrationEvent) (*beaconprotocol.Share, bool, error) {
	share, shareSecret, err := ShareFromValidatorEvent(
		validatorEvent,
		c.storage,
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
			return nil, isOperatorShare, errors.New("could not decode shareSecret")
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
	}

	// save validator data
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return nil, isOperatorShare, errors.Wrap(err, "could not save validator share")
	}

	return share, isOperatorShare, nil
}

// onShareRemove is called when a validator was removed
// TODO: think how we can make this function atomic (i.e. failing wouldn't stop the removal of the share)
func (c *controller) onShareRemove(pk string, removeSecret bool) error {
	// remove from validatorsMap
	v := c.validatorsMap.RemoveValidator(pk)

	// stop instance
	if v != nil {
		if err := v.Close(); err != nil {
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

func (c *controller) onShareStart(share *beaconprotocol.Share) {
	v := c.validatorsMap.GetOrCreateValidator(share)
	_, err := c.startValidator(v)
	if err != nil {
		c.logger.Warn("could not start validator", zap.Error(err))
	}
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
	if err := v.Start(); err != nil {
		metricsValidatorStatus.WithLabelValues(v.GetShare().PublicKey.SerializeToHexStr()).Set(float64(validatorStatusError))
		return false, errors.Wrap(err, "could not start validator")
	}
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop() {
	go c.metadataUpdateQueue.Start()

	for {
		time.Sleep(c.metadataUpdateInterval)

		shares, err := c.collection.GetOperatorValidatorShares(c.operatorPubKey, true)
		if err != nil {
			c.logger.Warn("could not get validators shares for metadata update", zap.Error(err))
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
