package validator

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	utilsprotocol "github.com/bloxapp/ssv/protocol/v1/queue"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"github.com/bloxapp/ssv/protocol/v1/sync/handlers"
	v1types "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v2/commons"
	controller2 "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	validatorv2 "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/tasks"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	metadataBatchSize = 25
)

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *spectypes.Share)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncRateLimit       time.Duration `yaml:"HistorySyncRateLimit" env:"HISTORY_SYNC_BACKOFF" env-default:"200ms" env-description:"Interval for updating metadata"`
	MinPeers                   int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"The required minimum peers for sync"`
	ETHNetwork                 beaconprotocol.Network
	Network                    network.P2PNetwork
	Beacon                     beaconprotocol.Beacon
	Shares                     []ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider ShareEncryptionKeyProvider
	CleanRegistryData          bool
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Flag that indicates whether the node saves decided history or just the latest messages"`
	KeyManager                 spectypes.KeyManager
	OperatorPubKey             string
	RegistryStorage            registrystorage.OperatorsCollection
	ForkVersion                forksprotocol.ForkVersion
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole

	// worker flags
	WorkersCount    int `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"4096" env-description:"Number of goroutines to use for message workers"`
	QueueBufferSize int `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"1024" env-description:"Buffer size for message workers"`
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	ListenToEth1Events(feed *event.Feed)
	StartValidators()
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (*validatorv2.Validator, bool)
	UpdateValidatorMetaDataLoop()
	StartNetworkHandlers()
	Eth1EventHandler(ongoingSync bool) eth1.SyncEventHandler
	GetAllValidatorShares() ([]*types.SSVShare, error)
	// GetValidatorStats returns stats of validators, including the following:
	//  - the amount of validators in the network
	//  - the amount of active validators (i.e. not slashed or existed)
	//  - the amount of validators assigned to this operator
	GetValidatorStats() (uint64, uint64, uint64, error)
	//OnFork(forkVersion forksprotocol.ForkVersion) error
}

// controller implements Controller
type controller struct {
	context     context.Context
	collection  ICollection
	storage     registrystorage.OperatorsCollection
	ibftStorage qbftstorage.QBFTStore
	logger      *zap.Logger
	beacon      beaconprotocol.Beacon
	keyManager  spectypes.KeyManager

	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	operatorPubKey             string

	validatorsMap    *validatorsMap
	validatorOptions *validatorv2.Options

	metadataUpdateQueue    utilsprotocol.Queue
	metadataUpdateInterval time.Duration

	operatorsIDs  *sync.Map
	network       network.P2PNetwork
	forkVersion   forksprotocol.ForkVersion
	messageRouter *messageRouter
	messageWorker *worker.Worker
}

// NewController creates a new validator controller instance
func NewController(options ControllerOptions) Controller {
	collection := NewCollection(CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	qbftStorage := storage.New(options.DB, options.Logger, spectypes.BNRoleAttester.String(), options.ForkVersion) // TODO need to support multi duties

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	msgID := forksfactory.NewFork(options.ForkVersion).MsgID()

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		Logger:       options.Logger,
		WorkersCount: options.WorkersCount,
		Buffer:       options.QueueBufferSize,
	}

	validatorOptions := &validatorv2.Options{ //TODO add vars
		Logger:  options.Logger,
		Network: options.Network,
		Beacon:  commons.NewBeaconAdapter(options.Beacon),
		Storage: commons.NewQBFTStorageAdapter(qbftStorage), // should support more than one duty type
		//Share:   nil,  // set per validator
		Signer: options.KeyManager,
		//Mode: validatorv2.ModeRW // set per validator
		DutyRunners: nil, // set per validator
	}

	ctrl := controller{
		collection:                 collection,
		storage:                    options.RegistryStorage,
		ibftStorage:                qbftStorage,
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

		messageRouter: newMessageRouter(options.Logger, msgID),
		messageWorker: worker.NewWorker(workerCfg),
	}

	if err := ctrl.initShares(options); err != nil {
		ctrl.logger.Panic("could not initialize shares", zap.Error(err))
	}

	return &ctrl
}

// setupNetworkHandlers registers all the required handlers for sync protocols
func (c *controller) setupNetworkHandlers() error {
	c.network.RegisterHandlers(p2pprotocol.WithHandler(
		p2pprotocol.LastDecidedProtocol,
		handlers.LastDecidedHandler(c.logger, c.ibftStorage, c.network),
	), p2pprotocol.WithHandler(
		p2pprotocol.LastChangeRoundProtocol,
		handlers.LastChangeRoundHandler(c.logger, c.ibftStorage, c.network),
	), p2pprotocol.WithHandler(
		p2pprotocol.DecidedHistoryProtocol,
		// TODO: extract maxBatch to config
		handlers.HistoryHandler(c.logger, c.ibftStorage, c.network, 25),
	))
	return nil
}

func (c *controller) GetAllValidatorShares() ([]*types.SSVShare, error) {
	return c.collection.GetAllValidatorShares()
}

func (c *controller) GetValidatorStats() (uint64, uint64, uint64, error) {
	allShares, err := c.collection.GetAllValidatorShares()
	if err != nil {
		return 0, 0, 0, err
	}
	operatorShares := uint64(0)
	active := uint64(0)
	for _, s := range allShares {
		if ok := s.BelongsToOperator(c.operatorPubKey); ok {
			operatorShares++
		}
		if s.HasStats() && s.Stats.IsActive() {
			active++
		}
	}
	return uint64(len(allShares)), active, operatorShares, nil
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
			pk := msg.GetID().GetPubKey()
			hexPK := hex.EncodeToString(pk)

			if v, ok := c.validatorsMap.GetValidator(hexPK); ok {
				v.HandleMessage(&msg)
			} else {
				if msg.MsgType != message.SSVDecidedMsgType && msg.MsgType != spectypes.SSVConsensusMsgType {
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

func (c *controller) handleWorkerMessages(msg *spectypes.SSVMessage) error {
	share, err := c.getShare(msg.GetID().GetPubKey())
	if err != nil {
		return err
	}
	if share == nil {
		return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetID().GetPubKey()))
	}

	opts := *c.validatorOptions
	opts.Share = share
	opts.Mode = validatorv2.ModeR
	opts.DutyRunners = setupRunners(c.context, opts)

	v := validatorv2.NewValidator(c.context, opts)
	v.HandleMessage(msg)
	return v.Stop()
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
	shareList, err := c.collection.GetValidatorSharesByOperatorPK(c.operatorPubKey, true)
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shareList) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.setupValidators(shareList)
}

// setupValidators setup and starts validators from the given shares
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (c *controller) setupValidators(shares []*types.SSVShare) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var started int
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		v := c.validatorsMap.GetOrCreateValidator(validatorShare)
		pk := hex.EncodeToString(v.Share.ValidatorPubKey)
		logger := c.logger.With(zap.String("pubkey", pk))
		if !v.Share.HasStats() { // fetching index and status in case not exist
			fetchMetadata = append(fetchMetadata, v.Share.ValidatorPubKey)
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
	// first, set stream handlers
	if err := c.setupNetworkHandlers(); err != nil {
		c.logger.Panic("could not register stream handlers", zap.Error(err))
	}
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
		v.Share.Stats = metadata
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
func (c *controller) GetValidator(pubKey string) (*validatorv2.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices
// and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices() []spec.ValidatorIndex {
	var toFetch [][]byte
	var indices []spec.ValidatorIndex

	err := c.validatorsMap.ForEach(func(v *validatorv2.Validator) error {
		if !v.Share.HasStats() {
			toFetch = append(toFetch, v.Share.ValidatorPubKey)
		} else if v.Share.Stats.IsActive() { // eth-client throws error once trying to fetch duties for existed validator
			indices = append(indices, v.Share.Stats.Index)
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
		if !v.Share.HasStats() {
			v.Share.Stats = meta
			c.logger.Debug("metadata was updated", zap.String("pk", pk))
		} else if !v.Share.Stats.Equals(meta) {
			v.Share.Stats.Status = meta.Status
			v.Share.Stats.Balance = meta.Balance
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
func (c *controller) onShareCreate(validatorEvent abiparser.ValidatorRegistrationEvent) (*types.SSVShare, bool, error) {
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
	isOperatorMetadata := share.BelongsToOperator(c.operatorPubKey)

	if isOperatorMetadata {
		if shareSecret == nil {
			return nil, isOperatorMetadata, errors.New("could not decode shareSecret")
		}

		logger := c.logger.With(zap.String("pubKey", hex.EncodeToString(share.ValidatorPubKey)))

		// get metadata
		if updated, err := UpdateShareMetadata(share, c.beacon); err != nil {
			logger.Warn("could not add validator metadata", zap.Error(err))
		} else if !updated {
			logger.Warn("could not find validator metadata")
		}

		// save secret key
		if err := c.keyManager.AddShare(shareSecret); err != nil {
			return nil, isOperatorMetadata, errors.Wrap(err, "could not add share secret to key manager")
		}
	}

	// save validator data
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return nil, isOperatorMetadata, errors.Wrap(err, "could not save validator share")
	}

	return share, isOperatorMetadata, nil
}

// onShareRemove is called when a validator was removed
// TODO: think how we can make this function atomic (i.e. failing wouldn't stop the removal of the share)
func (c *controller) onShareRemove(pk string, removeSecret bool) error {
	// remove from validatorsMap
	v := c.validatorsMap.RemoveValidator(pk)

	// stop instance
	if v != nil {
		if err := v.Stop(); err != nil {
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

func (c *controller) onShareStart(share *types.SSVShare) {
	v := c.validatorsMap.GetOrCreateValidator(share)
	_, err := c.startValidator(v)
	if err != nil {
		c.logger.Warn("could not start validator", zap.Error(err))
	}
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *validatorv2.Validator) (bool, error) {
	if !v.Share.HasStats() {
		return false, errors.New("could not start validator: stats not found")
	}
	ReportValidatorStatus(hex.EncodeToString(v.Share.ValidatorPubKey), v.Share.Stats, c.logger)
	if v.Share.Stats.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	if err := v.Start(); err != nil {
		metricsValidatorStatus.WithLabelValues(hex.EncodeToString(v.Share.ValidatorPubKey)).Set(float64(validatorStatusError))
		return false, errors.Wrap(err, "could not start validator")
	}
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop() {
	go c.metadataUpdateQueue.Start()

	for {
		time.Sleep(c.metadataUpdateInterval)

		shareList, err := c.collection.GetValidatorSharesByOperatorPK(c.operatorPubKey, true)
		if err != nil {
			c.logger.Warn("could not get validators shares for metadata update", zap.Error(err))
			continue
		}
		var pks [][]byte
		for _, share := range shareList {
			pks = append(pks, share.ValidatorPubKey)
		}
		c.logger.Debug("updating metadata in loop", zap.Int("shares count", len(shareList)))
		beaconprotocol.UpdateValidatorsMetadataBatch(pks, c.metadataUpdateQueue, c,
			c.beacon, c.onMetadataUpdated, metadataBatchSize)
	}
}

func setupRunners(ctx context.Context, options validatorv2.Options) runner.DutyRunners {
	if options.Share.Stats == nil {
		options.Logger.Error("validator missing metadata", zap.String("pk", hex.EncodeToString(options.Share.ValidatorPubKey)))
		return runner.DutyRunners{} // TODO need to find better way to fix it
	}

	runnersType := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
	}

	domainType := v1types.GetDefaultDomain()
	generateConfig := func() *types.Config {
		return &types.Config{
			Signer:      options.Signer,
			SigningPK:   options.Share.ValidatorPubKey, // TODO right val?
			Domain:      domainType,
			ValueCheckF: nil, // sets per role type
			ProposerF: func(state *qbft.State, round qbft.Round) spectypes.OperatorID {
				leader := qbft.RoundRobinProposer(state, round)
				options.Logger.Debug("leader", zap.Int("", int(leader)))
				return leader
			},
			Storage: options.Storage,
			Network: options.Network,
			Timer:   roundtimer.New(ctx, options.Logger),
		}
	}

	runners := runner.DutyRunners{}
	for _, role := range runnersType {
		switch role {
		case spectypes.BNRoleAttester:
			valCheck := ssv.AttesterValueCheckF(options.Signer, spectypes.PraterNetwork, options.Share.ValidatorPubKey, options.Share.Stats.Index)
			config := generateConfig()
			config.ValueCheckF = valCheck
			identifier := spectypes.NewMsgID(options.Share.ValidatorPubKey, spectypes.BNRoleAttester)
			qbftQtrl := controller2.NewController(identifier[:], options.Share, domainType, config)
			runners[role] = runner.NewAttesterRunnner(spectypes.PraterNetwork, options.Share, qbftQtrl, options.Beacon, options.Network, options.Signer, valCheck)
		case spectypes.BNRoleProposer:
			proposedValueCheck := ssv.ProposerValueCheckF(options.Signer, spectypes.PraterNetwork, options.Share.ValidatorPubKey, options.Share.Stats.Index)
			config := generateConfig()
			config.ValueCheckF = proposedValueCheck
			identifier := spectypes.NewMsgID(options.Share.ValidatorPubKey, spectypes.BNRoleProposer)
			qbftQtrl := controller2.NewController(identifier[:], options.Share, domainType, config)
			runners[role] = runner.NewProposerRunner(spectypes.PraterNetwork, options.Share, qbftQtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck)
		case spectypes.BNRoleAggregator:
			proposedValueCheck := ssv.AggregatorValueCheckF(options.Signer, spectypes.PraterNetwork, options.Share.ValidatorPubKey, options.Share.Stats.Index)
			config := generateConfig()
			config.ValueCheckF = proposedValueCheck
			identifier := spectypes.NewMsgID(options.Share.ValidatorPubKey, spectypes.BNRoleAggregator)
			qbftQtrl := controller2.NewController(identifier[:], options.Share, domainType, config)
			runners[role] = runner.NewAggregatorRunner(spectypes.PraterNetwork, options.Share, qbftQtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck)
		case spectypes.BNRoleSyncCommittee:
			proposedValueCheck := ssv.SyncCommitteeValueCheckF(options.Signer, spectypes.PraterNetwork, options.Share.ValidatorPubKey, options.Share.Stats.Index)
			config := generateConfig()
			config.ValueCheckF = proposedValueCheck
			identifier := spectypes.NewMsgID(options.Share.ValidatorPubKey, spectypes.BNRoleSyncCommittee)
			qbftQtrl := controller2.NewController(identifier[:], options.Share, domainType, config)
			runners[role] = runner.NewSyncCommitteeRunner(spectypes.PraterNetwork, options.Share, qbftQtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck)
		case spectypes.BNRoleSyncCommitteeContribution:
			proposedValueCheck := ssv.SyncCommitteeContributionValueCheckF(options.Signer, spectypes.PraterNetwork, options.Share.ValidatorPubKey, options.Share.Stats.Index)
			config := generateConfig()
			config.ValueCheckF = proposedValueCheck
			identifier := spectypes.NewMsgID(options.Share.ValidatorPubKey, spectypes.BNRoleSyncCommitteeContribution)
			qbftQtrl := controller2.NewController(identifier[:], options.Share, domainType, config)
			runners[role] = runner.NewSyncCommitteeAggregatorRunner(spectypes.PraterNetwork, options.Share, qbftQtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck)
		}
	}
	return runners
}
