package validator

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	utilsprotocol "github.com/bloxapp/ssv/protocol/v2/queue"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/sync/handlers"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	metadataBatchSize        = 25
	networkRouterConcurrency = 2048
)

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *types.SSVShare)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncBatchSize       int           `yaml:"HistorySyncBatchSize" env:"HISTORY_SYNC_BATCH_SIZE" env-default:"25" env-description:"Maximum number of messages to sync in a single batch"`
	MinPeers                   int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"The required minimum peers for sync"`
	ETHNetwork                 beaconprotocol.Network
	Network                    network.P2PNetwork
	Beacon                     beaconprotocol.Beacon
	ShareEncryptionKeyProvider ShareEncryptionKeyProvider
	CleanRegistryData          bool
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Save decided history rather than just highest messages"`
	Exporter                   bool `yaml:"Exporter" env:"EXPORTER" env-default:"false" env-description:""`
	KeyManager                 spectypes.KeyManager
	OperatorPubKey             string
	RegistryStorage            nodestorage.Storage
	ForkVersion                forksprotocol.ForkVersion
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole

	// worker flags
	WorkersCount    int `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"512" env-description:"Number of goroutines to use for message workers"`
	QueueBufferSize int `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"1024" env-description:"Buffer size for message workers"`
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	ListenToEth1Events(logger *zap.Logger, feed *event.Feed)
	StartValidators(logger *zap.Logger)
	GetValidatorsIndices(logger *zap.Logger) []phase0.ValidatorIndex
	GetValidator(pubKey string) (*validator.Validator, bool)
	UpdateValidatorMetaDataLoop(logger *zap.Logger)
	StartNetworkHandlers(logger *zap.Logger)
	Eth1EventHandler(logger *zap.Logger, ongoingSync bool) eth1.SyncEventHandler
	GetAllValidatorShares(logger *zap.Logger) ([]*types.SSVShare, error)
	// GetValidatorStats returns stats of validators, including the following:
	//  - the amount of validators in the network
	//  - the amount of active validators (i.e. not slashed or existed)
	//  - the amount of validators assigned to this operator
	GetValidatorStats(logger *zap.Logger) (uint64, uint64, uint64, error)
	//OnFork(forkVersion forksprotocol.ForkVersion) error
}

// controller implements Controller
type controller struct {
	context              context.Context
	collection           ICollection
	operatorsCollection  registrystorage.OperatorsCollection
	recipientsCollection registrystorage.RecipientsCollection
	ibftStorageMap       *storage.QBFTStores

	beacon               beaconprotocol.Beacon
	keyManager           spectypes.KeyManager

	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	operatorPubKey             string

	validatorsMap    *validatorsMap
	validatorOptions *validator.Options

	metadataUpdateQueue    utilsprotocol.Queue
	metadataUpdateInterval time.Duration

	operatorsIDs         *sync.Map
	network              network.P2PNetwork
	forkVersion          forksprotocol.ForkVersion
	messageRouter        *messageRouter
	messageWorker        *worker.Worker
	historySyncBatchSize int

	// nonCommitteeLocks is a map of locks for non committee validators, used to ensure
	// messages with identical public key and role are processed one at a time.
	nonCommitteeLocks map[spectypes.MessageID]*sync.Mutex
	nonCommitteeMutex sync.Mutex
}

// NewController creates a new validator controller instance
func NewController(logger *zap.Logger, options ControllerOptions) Controller {
	collection := NewCollection(CollectionOptions{
		DB: options.DB,
	})

	logger.Debug("CreatingController", zap.Bool("full_node", options.FullNode))
	storageMap := storage.NewStores()
	storageMap.Add(spectypes.BNRoleAttester, storage.New(options.DB, spectypes.BNRoleAttester.String(), options.ForkVersion))
	storageMap.Add(spectypes.BNRoleProposer, storage.New(options.DB, spectypes.BNRoleProposer.String(), options.ForkVersion))
	storageMap.Add(spectypes.BNRoleAggregator, storage.New(options.DB, spectypes.BNRoleAggregator.String(), options.ForkVersion))
	storageMap.Add(spectypes.BNRoleSyncCommittee, storage.New(options.DB, spectypes.BNRoleSyncCommittee.String(), options.ForkVersion))
	storageMap.Add(spectypes.BNRoleSyncCommitteeContribution, storage.New(options.DB, spectypes.BNRoleSyncCommitteeContribution.String(), options.ForkVersion))

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	msgID := forksfactory.NewFork(options.ForkVersion).MsgID()

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		WorkersCount: options.WorkersCount,
		Buffer:       options.QueueBufferSize,
	}

	validatorOptions := &validator.Options{ //TODO add vars
		Network: options.Network,
		Beacon:  options.Beacon,
		Storage: storageMap,
		//Share:   nil,  // set per validator
		Signer: options.KeyManager,
		//Mode: validator.ModeRW // set per validator
		DutyRunners:       nil, // set per validator
		NewDecidedHandler: options.NewDecidedHandler,
		FullNode:          options.FullNode,
		Exporter:          options.Exporter,
	}

	// If full node, increase queue size to make enough room
	// for history sync batches to be pushed whole.
	if options.FullNode {
		size := options.HistorySyncBatchSize * 2
		if size > validator.DefaultQueueSize {
			validatorOptions.QueueSize = size
		}
	}

	ctrl := controller{
		collection:                 collection,
		operatorsCollection:        options.RegistryStorage,
		recipientsCollection:       options.RegistryStorage,
		ibftStorageMap:             storageMap,
		context:                    options.Context,
		beacon:                     options.Beacon,
		shareEncryptionKeyProvider: options.ShareEncryptionKeyProvider,
		operatorPubKey:             options.OperatorPubKey,
		keyManager:                 options.KeyManager,
		network:                    options.Network,
		forkVersion:                options.ForkVersion,

		validatorsMap:    newValidatorsMap(options.Context, options.DB, validatorOptions),
		validatorOptions: validatorOptions,

		metadataUpdateQueue:    tasks.NewExecutionQueue(10 * time.Millisecond),
		metadataUpdateInterval: options.MetadataUpdateInterval,

		operatorsIDs: operatorsIDs,

		messageRouter:        newMessageRouter(msgID),
		messageWorker:        worker.NewWorker(logger, workerCfg),
		historySyncBatchSize: options.HistorySyncBatchSize,

		nonCommitteeLocks: make(map[spectypes.MessageID]*sync.Mutex),
	}

	// TODO(oleg) chk if needed
	if err := ctrl.initShares(logger, options); err != nil {
		logger.Panic("could not initialize shares", zap.Error(err))
	}

	return &ctrl
}

// setupNetworkHandlers registers all the required handlers for sync protocols
func (c *controller) setupNetworkHandlers(logger *zap.Logger) error {
	syncHandlers := []*p2pprotocol.SyncHandler{
		p2pprotocol.WithHandler(
			p2pprotocol.LastDecidedProtocol,
			handlers.LastDecidedHandler(logger, c.ibftStorageMap, c.network),
		),
	}
	if c.validatorOptions.FullNode {
		syncHandlers = append(
			syncHandlers,
			p2pprotocol.WithHandler(
				p2pprotocol.DecidedHistoryProtocol,
				// TODO: extract maxBatch to config
				handlers.HistoryHandler(logger, c.ibftStorageMap, c.network, c.historySyncBatchSize),
			),
		)
	}
	logger.Debug("setting up network handlers",
		zap.Int("count", len(syncHandlers)),
		zap.Bool("full_node", c.validatorOptions.FullNode),
		zap.Bool("exporter", c.validatorOptions.Exporter),
		zap.Int("queue_size", c.validatorOptions.QueueSize))
	c.network.RegisterHandlers(logger, syncHandlers...)
	return nil
}

func (c *controller) GetAllValidatorShares(logger *zap.Logger) ([]*types.SSVShare, error) {
	return c.collection.GetAllValidatorShares(logger)
}

func (c *controller) GetValidatorStats(logger *zap.Logger) (uint64, uint64, uint64, error) {
	allShares, err := c.collection.GetAllValidatorShares(logger)
	if err != nil {
		return 0, 0, 0, err
	}
	operatorShares := uint64(0)
	active := uint64(0)
	for _, s := range allShares {
		if ok := s.BelongsToOperator(c.operatorPubKey); ok {
			operatorShares++
		}
		if s.HasBeaconMetadata() && s.BeaconMetadata.IsActive() {
			active++
		}
	}
	return uint64(len(allShares)), active, operatorShares, nil
}

func (c *controller) handleRouterMessages(logger *zap.Logger) {
	ctx, cancel := context.WithCancel(c.context)
	defer cancel()
	ch := c.messageRouter.GetMessageChan()

	for {
		select {
		case <-ctx.Done():
			logger.Debug("router message handler stopped")
			return
		case msg := <-ch:
			// TODO temp solution to prevent getting event msgs from network. need to to add validation in p2p
			if msg.MsgType == message.SSVEventMsgType {
				continue
			}

			pk := msg.GetID().GetPubKey()
			hexPK := hex.EncodeToString(pk)
			if v, ok := c.validatorsMap.GetValidator(hexPK); ok {
				v.HandleMessage(logger, &msg)
			} else {
				if msg.MsgType != spectypes.SSVConsensusMsgType {
					continue // not supporting other types
				}
				if !c.messageWorker.TryEnqueue(&msg) { // start to save non committee decided messages only post fork
					logger.Warn("Failed to enqueue post consensus message: buffer is full")
				}
			}
		}
	}
}

// getShare returns the share of the given validator public key
// TODO: optimize
func (c *controller) getShare(pk spectypes.ValidatorPK) (*types.SSVShare, error) {
	share, found, err := c.collection.GetValidatorShare(pk)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read validator share [%s]", pk)
	}
	if !found {
		return nil, nil
	}
	return share, nil
}

func (c *controller) handleWorkerMessages(logger *zap.Logger, msg *spectypes.SSVMessage) error {
	share, err := c.getShare(msg.GetID().GetPubKey())
	if err != nil {
		return err
	}
	if share == nil {
		return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetID().GetPubKey()))
	}

	opts := *c.validatorOptions
	opts.SSVShare = share

	// Lock this message ID.
	lock := func() *sync.Mutex {
		c.nonCommitteeMutex.Lock()
		defer c.nonCommitteeMutex.Unlock()
		if _, ok := c.nonCommitteeLocks[msg.GetID()]; !ok {
			c.nonCommitteeLocks[msg.GetID()] = &sync.Mutex{}
		}
		return c.nonCommitteeLocks[msg.GetID()]
	}()

	lock.Lock()
	defer lock.Unlock()

	// Create a disposable NonCommitteeValidator to process the message.
	// TODO: consider caching highest instance heights for each validator instead of creating a new validator & loading from storage each time.
	v := validator.NewNonCommitteeValidator(msg.GetID(), opts)
	v.ProcessMessage(logger, msg)

	return nil
}

// ListenToEth1Events is listening to events coming from eth1 client
func (c *controller) ListenToEth1Events(logger *zap.Logger, feed *event.Feed) {
	cn := make(chan *eth1.Event)
	sub := feed.Subscribe(cn)
	defer sub.Unsubscribe()

	handler := c.Eth1EventHandler(logger, true)

	for {
		select {
		case e := <-cn:
			logFields, err := handler(*e)
			_ = eth1.HandleEventResult(logger, *e, logFields, err, true)
		case err := <-sub.Err():
			logger.Warn("event feed subscription error", zap.Error(err))
		}
	}
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators(logger *zap.Logger) {
	if c.validatorOptions.Exporter {
		c.setupNonCommitteeValidators(logger)
		return
	}

	shares, err := c.getValidators(logger)
	if err != nil {
		logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		logger.Info("could not find validators")
		return
	}
	c.setupValidators(logger, shares)
}

func (c *controller) getValidators(logger *zap.Logger) ([]*types.SSVShare, error) {
	return c.collection.GetFilteredValidatorShares(logger, NotLiquidatedAndByOperatorPubKey(c.operatorPubKey))
}

// setupValidators setup and starts validators from the given shares.
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (c *controller) setupValidators(logger *zap.Logger, shares []*types.SSVShare) {
	logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var started int
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		isStarted, err := c.onShareStart(validatorShare)
		if err != nil {
			logger.Warn("could not start validator", zap.String("pubkey", hex.EncodeToString(validatorShare.ValidatorPubKey)), zap.Error(err))
			errs = append(errs, err)
		}
		if !isStarted && err == nil {
			// Fetch metadata, if needed.
			fetchMetadata = append(fetchMetadata, validatorShare.ValidatorPubKey)
		}
		if isStarted {
			started++
		}
	}
	logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.Size()),
		zap.Int("failures", len(errs)), zap.Int("missing metadata", len(fetchMetadata)),
		zap.Int("shares count", len(shares)), zap.Int("started", started))

	go c.updateValidatorsMetadata(logger, fetchMetadata)
}

// setupNonCommitteeValidators trigger SyncHighestDecided for each validator
// to start consensus flow which would save the highest decided instance
// and sync any gaps (in protocol/v2/qbft/controller/decided.go).
func (c *controller) setupNonCommitteeValidators(logger *zap.Logger) {
	nonCommitteeShares, err := c.collection.GetFilteredValidatorShares(logger, NotLiquidated())
	if err != nil {
		logger.Fatal("failed to get non-committee validator shares", zap.Error(err))
	}
	if len(nonCommitteeShares) == 0 {
		logger.Info("could not find non-committee validators")
		return
	}

	for _, validatorShare := range nonCommitteeShares {
		opts := *c.validatorOptions
		opts.SSVShare = validatorShare
		allRoles := []spectypes.BeaconRole{
			spectypes.BNRoleAttester,
			spectypes.BNRoleAggregator,
			spectypes.BNRoleProposer,
			spectypes.BNRoleSyncCommittee,
			spectypes.BNRoleSyncCommitteeContribution,
		}
		for _, role := range allRoles {
			role := role
			err := c.network.Subscribe(validatorShare.ValidatorPubKey)
			if err != nil {
				logger.Error("failed to subscribe to network", zap.Error(err))
			}
			messageID := spectypes.NewMsgID(validatorShare.ValidatorPubKey, role)
			err = c.network.SyncHighestDecided(messageID)
			if err != nil {
				logger.Error("failed to sync highest decided", zap.Error(err))
			}
		}
	}
}

// StartNetworkHandlers init msg worker that handles network messages
func (c *controller) StartNetworkHandlers(logger *zap.Logger) {
	// first, set stream handlers
	if err := c.setupNetworkHandlers(logger); err != nil {
		logger.Panic("could not register stream handlers", zap.Error(err))
	}
	c.network.UseMessageRouter(c.messageRouter)
	for i := 0; i < networkRouterConcurrency; i++ {
		go c.handleRouterMessages(logger)
	}
	c.messageWorker.UseHandler(c.handleWorkerMessages)
}

// updateValidatorsMetadata updates metadata of the given public keys.
// as part of the flow in beacon.UpdateValidatorsMetadata,
// UpdateValidatorMetadata is called to persist metadata and start a specific validator
func (c *controller) updateValidatorsMetadata(logger *zap.Logger, pubKeys [][]byte) {
	if len(pubKeys) > 0 {
		logger.Debug("updating validators", zap.Int("count", len(pubKeys)))
		if err := beaconprotocol.UpdateValidatorsMetadata(logger, pubKeys, c, c.beacon, c.onMetadataUpdated); err != nil {
			logger.Warn("could not update all validators", zap.Error(err))
		}
	}
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(logger *zap.Logger, pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.Share.BeaconMetadata = metadata
		if err := c.collection.(beaconprotocol.ValidatorMetadataStorage).UpdateValidatorMetadata(logger, pk, metadata); err != nil {
			return err
		}
		_, err := c.startValidator(logger, v)
		if err != nil {
			logger.Warn("could not start validator", zap.Error(err))
		}
	}
	return nil
}

// GetValidator returns a validator instance from validatorsMap
func (c *controller) GetValidator(pubKey string) (*validator.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices
// and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices(logger *zap.Logger) []phase0.ValidatorIndex {
	var toFetch [][]byte
	var indices []phase0.ValidatorIndex

	err := c.validatorsMap.ForEach(func(v *validator.Validator) error {
		if !v.Share.HasBeaconMetadata() {
			toFetch = append(toFetch, v.Share.ValidatorPubKey)
		} else if v.Share.BeaconMetadata.IsActive() { // eth-client throws error once trying to fetch duties for existed validator
			indices = append(indices, v.Share.BeaconMetadata.Index)
		}
		return nil
	})
	if err != nil {
		logger.Warn("failed to get all validators public keys", zap.Error(err))
	}

	go c.updateValidatorsMetadata(logger, toFetch)

	return indices
}

// onMetadataUpdated is called when validator's metadata was updated
func (c *controller) onMetadataUpdated(logger *zap.Logger, pk string, meta *beaconprotocol.ValidatorMetadata) {
	if meta == nil {
		return
	}
	if v, exist := c.GetValidator(pk); exist {
		// update share object owned by the validator
		// TODO: check if this updates running validators
		if !v.Share.HasBeaconMetadata() {
			v.Share.BeaconMetadata = meta
			logger.Debug("metadata was updated", zap.String("pk", pk))
		} else if !v.Share.BeaconMetadata.Equals(meta) {
			v.Share.BeaconMetadata.Status = meta.Status
			v.Share.BeaconMetadata.Balance = meta.Balance
			logger.Debug("metadata was updated", zap.String("pk", pk))
		}
		_, err := c.startValidator(logger, v)
		if err != nil {
			logger.Warn("could not start validator after metadata update",
				zap.String("pk", pk), zap.Error(err), zap.Any("metadata", meta))
		}
	}
}

// onShareCreate is called when a validator was added/updated during registry sync
func (c *controller) onShareCreate(logger *zap.Logger, validatorEvent abiparser.ValidatorAddedEvent) (*types.SSVShare, bool, error) {
	share, shareSecret, err := ShareFromValidatorEvent(
		validatorEvent,
		c.operatorsCollection,
		c.shareEncryptionKeyProvider,
		c.operatorPubKey,
	)
	if err != nil {
		return nil, false, errors.Wrap(err, "could not extract validator share from event")
	}

	// determine if the share belongs to operator
	isOperatorShare := share.BelongsToOperator(c.operatorPubKey)

	if isOperatorShare {
		if shareSecret == nil {
			return nil, isOperatorShare, errors.New("could not decode shareSecret")
		}

		logger := logger.With(zap.String("pubKey", hex.EncodeToString(share.ValidatorPubKey)))

		// TODO(oleg): should we care about non-committee podIds?
		if err = share.SetPodID(); err != nil {
			return nil, false, errors.Wrap(err, "could not set share pod id")
		}

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
	if err := c.collection.SaveValidatorShare(logger, share); err != nil {
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
		v.Stop()
	}
	// remove the share secret from key-manager
	if removeSecret {
		if err := c.keyManager.RemoveShare(pk); err != nil {
			return errors.Wrap(err, "could not remove share secret from key manager")
		}
	}

	return nil
}

func (c *controller) onShareStart(logger *zap.logger, share *types.SSVShare) (bool, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		logger.Warn("could not start validator as metadata not found", zap.String("pubkey", hex.EncodeToString(share.ValidatorPubKey)))
		return false, nil
	}

	// Start a committee validator.
	v := c.validatorsMap.GetOrCreateValidator(logger.Named("validatorsMap"), share)
	return c.startValidator(logger, v)
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(logger *zap.Logger, v *validator.Validator) (bool, error) {
	ReportValidatorStatus(hex.EncodeToString(v.Share.ValidatorPubKey), v.Share.BeaconMetadata, logger)
	if !v.Share.HasBeaconMetadata() {
		return false, errors.New("could not start validator: beacon metadata not found")
	}
	if v.Share.BeaconMetadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	if err := v.Start(logger); err != nil {
		metricsValidatorStatus.WithLabelValues(hex.EncodeToString(v.Share.ValidatorPubKey)).Set(float64(validatorStatusError))
		return false, errors.Wrap(err, "could not start validator")
	}
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop(logger *zap.Logger) {
	go c.metadataUpdateQueue.Start()

	for {
		time.Sleep(c.metadataUpdateInterval)

		shares, err := c.collection.GetFilteredValidatorShares(logger, NotLiquidatedAndByOperatorPubKey(c.operatorPubKey))
		if err != nil {
			logger.Warn("could not get validators shares for metadata update", zap.Error(err))
			continue
		}
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.ValidatorPubKey)
		}
		logger.Debug("updating metadata in loop", zap.Int("shares count", len(shares)))
		beaconprotocol.UpdateValidatorsMetadataBatch(logger, pks, c.metadataUpdateQueue, c,
			c.beacon, c.onMetadataUpdated, metadataBatchSize)
	}
}

// SetupRunners initializes duty runners for the given validator
func SetupRunners(ctx context.Context, logger *zap.Logger, options validator.Options) runner.DutyRunners {
	if options.SSVShare == nil || options.SSVShare.BeaconMetadata == nil {
		logger.Error("missing validator metadata", zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey)))
		return runner.DutyRunners{} // TODO need to find better way to fix it
	}

	runnersType := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
	}

	domainType := types.GetDefaultDomain()
	buildController := func(role spectypes.BeaconRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			Signer:      options.Signer,
			SigningPK:   options.SSVShare.ValidatorPubKey, // TODO right val?
			Domain:      domainType,
			ValueCheckF: nil, // sets per role type
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := specqbft.RoundRobinProposer(state, round)
				//logger.Debug("leader", zap.Int("operator_id", int(leader)))
				return leader
			},
			Storage: options.Storage.Get(role),
			Network: options.Network,
			Timer:   roundtimer.New(ctx, nil),
		}
		config.ValueCheckF = valueCheckF

		identifier := spectypes.NewMsgID(options.SSVShare.Share.ValidatorPubKey, role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], &options.SSVShare.Share, domainType, config, options.FullNode)
		qbftCtrl.NewDecidedHandler = options.NewDecidedHandler
		return qbftCtrl
	}

	runners := runner.DutyRunners{}
	for _, role := range runnersType {
		switch role {
		case spectypes.BNRoleAttester:
			valCheck := specssv.AttesterValueCheckF(options.Signer, spectypes.PraterNetwork, options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.BNRoleAttester, valCheck)
			runners[role] = runner.NewAttesterRunnner(spectypes.PraterNetwork, &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, valCheck)
		case spectypes.BNRoleProposer:
			proposedValueCheck := specssv.ProposerValueCheckF(options.Signer, spectypes.PraterNetwork, options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.BNRoleProposer, proposedValueCheck)
			runners[role] = runner.NewProposerRunner(spectypes.PraterNetwork, &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck)
		case spectypes.BNRoleAggregator:
			aggregatorValueCheckF := specssv.AggregatorValueCheckF(options.Signer, spectypes.PraterNetwork, options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleAggregator, aggregatorValueCheckF)
			runners[role] = runner.NewAggregatorRunner(spectypes.PraterNetwork, &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, aggregatorValueCheckF)
		case spectypes.BNRoleSyncCommittee:
			syncCommitteeValueCheckF := specssv.SyncCommitteeValueCheckF(options.Signer, spectypes.PraterNetwork, options.SSVShare.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleSyncCommittee, syncCommitteeValueCheckF)
			runners[role] = runner.NewSyncCommitteeRunner(spectypes.PraterNetwork, &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, syncCommitteeValueCheckF)
		case spectypes.BNRoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := specssv.SyncCommitteeContributionValueCheckF(options.Signer, spectypes.PraterNetwork, options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role] = runner.NewSyncCommitteeAggregatorRunner(spectypes.PraterNetwork, &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, syncCommitteeContributionValueCheckF)
		}
	}
	return runners
}
