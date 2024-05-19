package validator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jellydator/ttlcache/v3"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	networkRouterConcurrency = 2048
)

type GetRecipientDataFunc func(r basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *ssvtypes.SSVShare)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.Database
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncBatchSize       int           `yaml:"HistorySyncBatchSize" env:"HISTORY_SYNC_BATCH_SIZE" env-default:"25" env-description:"Maximum number of messages to sync in a single batch"`
	MinPeers                   int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"The required minimum peers for sync"`
	BeaconNetwork              beaconprotocol.Network
	Network                    P2PNetwork
	Beacon                     beaconprotocol.BeaconNode
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Save decided history rather than just highest messages"`
	Exporter                   bool `yaml:"Exporter" env:"EXPORTER" env-default:"false" env-description:""`
	KeyManager                 spectypes.KeyManager
	OperatorDataStore          operatordatastore.OperatorDataStore
	RegistryStorage            nodestorage.Storage
	RecipientsStorage          Recipients
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole
	StorageMap                 *storage.QBFTStores
	Metrics                    validator.Metrics
	MessageValidator           validation.MessageValidator
	ValidatorsMap              *validatorsmap.ValidatorsMap

	// worker flags
	WorkersCount    int `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"256" env-description:"Number of goroutines to use for message workers"`
	QueueBufferSize int `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"1024" env-description:"Buffer size for message workers"`
	GasLimit        uint64
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	StartValidators()
	CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	AllActiveIndices(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex
	GetValidator(pubKey string) (*validator.Validator, bool)
	ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty)
	UpdateValidatorMetaDataLoop()
	StartNetworkHandlers()
	GetOperatorShares() []*ssvtypes.SSVShare
	// GetValidatorStats returns stats of validators, including the following:
	//  - the amount of validators in the network
	//  - the amount of active validators (i.e. not slashed or existed)
	//  - the amount of validators assigned to this operator
	GetValidatorStats() (uint64, uint64, uint64, error)
	IndicesChangeChan() chan struct{}
	ValidatorExitChan() <-chan duties.ExitDescriptor

	StartValidator(share *ssvtypes.SSVShare) error
	StopValidator(pubKey spectypes.ValidatorPK) error
	LiquidateCluster(owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner common.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient common.Address) error
	ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex) error
}

type nonCommitteeValidator struct {
	*validator.NonCommitteeValidator
	sync.Mutex
}

type Nonce uint16

type Recipients interface {
	GetRecipientData(r basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error)
}

type SharesStorage interface {
	Get(txn basedb.Reader, pubKey []byte) *types.SSVShare
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*types.SSVShare
	UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error
}

type P2PNetwork interface {
	Broadcast(message *spectypes.SSVMessage) error
	UseMessageRouter(router network.MessageRouter)
	Peers(pk spectypes.ValidatorPK) ([]peer.ID, error)
	SubscribeRandoms(logger *zap.Logger, numSubnets int) error
	RegisterHandlers(logger *zap.Logger, handlers ...*p2pprotocol.SyncHandler)
}

// controller implements Controller
type controller struct {
	context context.Context

	logger  *zap.Logger
	metrics validator.Metrics

	sharesStorage     SharesStorage
	operatorsStorage  registrystorage.Operators
	recipientsStorage Recipients
	ibftStorageMap    *storage.QBFTStores

	beacon     beaconprotocol.BeaconNode
	keyManager spectypes.KeyManager

	operatorDataStore operatordatastore.OperatorDataStore

	validatorOptions        validator.Options
	validatorsMap           *validatorsmap.ValidatorsMap
	validatorStartFunc      func(validator *validator.Validator) (bool, error)
	committeeValidatorSetup chan struct{}

	metadataUpdateInterval time.Duration

	operatorsIDs         *sync.Map
	network              P2PNetwork
	messageRouter        *messageRouter
	messageWorker        *worker.Worker
	historySyncBatchSize int
	messageValidator     validation.MessageValidator

	// nonCommittees is a cache of initialized nonCommitteeValidator instances
	nonCommitteeValidators *ttlcache.Cache[spectypes.MessageID, *nonCommitteeValidator]
	nonCommitteeMutex      sync.Mutex

	recentlyStartedValidators uint64
	metadataLastUpdated       map[string]time.Time
	indicesChange             chan struct{}
	validatorExitCh           chan duties.ExitDescriptor
}

// NewController creates a new validator controller instance
func NewController(logger *zap.Logger, options ControllerOptions) Controller {
	logger.Debug("setting up validator controller")

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		WorkersCount: options.WorkersCount,
		Buffer:       options.QueueBufferSize,
	}

	validatorOptions := validator.Options{ //TODO add vars
		Network:       options.Network,
		Beacon:        options.Beacon,
		BeaconNetwork: options.BeaconNetwork.GetNetwork(),
		Storage:       options.StorageMap,
		//Share:   nil,  // set per validator
		Signer: options.KeyManager,
		//Mode: validator.ModeRW // set per validator
		DutyRunners:       nil, // set per validator
		NewDecidedHandler: options.NewDecidedHandler,
		FullNode:          options.FullNode,
		Exporter:          options.Exporter,
		GasLimit:          options.GasLimit,
		MessageValidator:  options.MessageValidator,
		Metrics:           options.Metrics,
	}

	// If full node, increase queue size to make enough room
	// for history sync batches to be pushed whole.
	if options.FullNode {
		size := options.HistorySyncBatchSize * 2
		if size > validator.DefaultQueueSize {
			validatorOptions.QueueSize = size
		}
	}

	metrics := validator.Metrics(validator.NopMetrics{})
	if options.Metrics != nil {
		metrics = options.Metrics
	}

	ctrl := controller{
		logger:            logger.Named(logging.NameController),
		metrics:           metrics,
		sharesStorage:     options.RegistryStorage.Shares(),
		operatorsStorage:  options.RegistryStorage,
		recipientsStorage: options.RegistryStorage,
		ibftStorageMap:    options.StorageMap,
		context:           options.Context,
		beacon:            options.Beacon,
		operatorDataStore: options.OperatorDataStore,
		keyManager:        options.KeyManager,
		network:           options.Network,

		validatorsMap:    options.ValidatorsMap,
		validatorOptions: validatorOptions,

		metadataUpdateInterval: options.MetadataUpdateInterval,

		operatorsIDs: operatorsIDs,

		messageRouter:        newMessageRouter(logger),
		messageWorker:        worker.NewWorker(logger, workerCfg),
		historySyncBatchSize: options.HistorySyncBatchSize,

		nonCommitteeValidators: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *nonCommitteeValidator](time.Minute * 13),
		),
		metadataLastUpdated:     make(map[string]time.Time),
		indicesChange:           make(chan struct{}),
		validatorExitCh:         make(chan duties.ExitDescriptor),
		committeeValidatorSetup: make(chan struct{}, 1),

		messageValidator: options.MessageValidator,
	}

	// Start automatic expired item deletion in nonCommitteeValidators.
	go ctrl.nonCommitteeValidators.Start()

	return &ctrl
}

// setupNetworkHandlers registers all the required handlers for sync protocols
func (c *controller) setupNetworkHandlers() error {
	syncHandlers := []*p2pprotocol.SyncHandler{}
	c.logger.Debug("setting up network handlers",
		zap.Int("count", len(syncHandlers)),
		zap.Bool("full_node", c.validatorOptions.FullNode),
		zap.Bool("exporter", c.validatorOptions.Exporter),
		zap.Int("queue_size", c.validatorOptions.QueueSize))
	c.network.RegisterHandlers(c.logger, syncHandlers...)
	return nil
}

func (c *controller) GetOperatorShares() []*ssvtypes.SSVShare {
	return c.sharesStorage.List(
		nil,
		registrystorage.ByOperatorID(c.operatorDataStore.GetOperatorID()),
		registrystorage.ByActiveValidator(),
	)
}

func (c *controller) IndicesChangeChan() chan struct{} {
	return c.indicesChange
}

func (c *controller) ValidatorExitChan() <-chan duties.ExitDescriptor {
	return c.validatorExitCh
}

func (c *controller) GetValidatorStats() (uint64, uint64, uint64, error) {
	allShares := c.sharesStorage.List(nil)
	operatorShares := uint64(0)
	active := uint64(0)
	for _, s := range allShares {
		if ok := s.BelongsToOperator(c.operatorDataStore.GetOperatorID()); ok {
			operatorShares++
		}
		if s.IsAttesting(c.beacon.GetBeaconNetwork().EstimatedCurrentEpoch()) {
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
			// TODO temp solution to prevent getting event msgs from network. need to to add validation in p2p
			if msg.MsgType == message.SSVEventMsgType {
				continue
			}

			pk := msg.GetID().GetPubKey()
			hexPK := hex.EncodeToString(pk)
			if v, ok := c.validatorsMap.GetValidator(hexPK); ok {
				v.HandleMessage(c.logger, msg)
			} else if c.validatorOptions.Exporter {
				if msg.MsgType != spectypes.SSVConsensusMsgType {
					continue // not supporting other types
				}
				if !c.messageWorker.TryEnqueue(msg) { // start to save non committee decided messages only post fork
					c.logger.Warn("Failed to enqueue post consensus message: buffer is full")
				}
			}
		}
	}
}

var nonCommitteeValidatorTTLs = map[spectypes.BeaconRole]phase0.Slot{
	spectypes.BNRoleAttester:                  64,
	spectypes.BNRoleProposer:                  4,
	spectypes.BNRoleAggregator:                4,
	spectypes.BNRoleSyncCommittee:             4,
	spectypes.BNRoleSyncCommitteeContribution: 4,
}

func (c *controller) handleWorkerMessages(msg *queue.DecodedSSVMessage) error {
	// Get or create a nonCommitteeValidator for this MessageID, and lock it to prevent
	// other handlers from processing
	var ncv *nonCommitteeValidator
	err := func() error {
		c.nonCommitteeMutex.Lock()
		defer c.nonCommitteeMutex.Unlock()

		item := c.nonCommitteeValidators.Get(msg.GetID())
		if item != nil {
			ncv = item.Value()
		} else {
			// Create a new nonCommitteeValidator and cache it.
			share := c.sharesStorage.Get(nil, msg.GetID().GetPubKey())
			if share == nil {
				return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetID().GetPubKey()))
			}

			opts := c.validatorOptions
			opts.SSVShare = share
			ncv = &nonCommitteeValidator{
				NonCommitteeValidator: validator.NewNonCommitteeValidator(c.logger, msg.GetID(), opts),
			}

			ttlSlots := nonCommitteeValidatorTTLs[msg.MsgID.GetRoleType()]
			c.nonCommitteeValidators.Set(
				msg.GetID(),
				ncv,
				time.Duration(ttlSlots)*c.beacon.GetBeaconNetwork().SlotDurationSec(),
			)
		}

		ncv.Lock()
		return nil
	}()
	if err != nil {
		return err
	}

	// Process the message.
	defer ncv.Unlock()
	ncv.ProcessMessage(c.logger, msg)

	return nil
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators() {
	if c.validatorOptions.Exporter {
		// There are no committee validators to setup.
		close(c.committeeValidatorSetup)

		// Setup non-committee validators.
		c.setupNonCommitteeValidators()
		return
	}

	shares := c.sharesStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}

	var ownShares []*ssvtypes.SSVShare
	var allPubKeys = make([][]byte, 0, len(shares))
	for _, share := range shares {
		if share.BelongsToOperator(c.operatorDataStore.GetOperatorID()) {
			ownShares = append(ownShares, share)
		}
		allPubKeys = append(allPubKeys, share.ValidatorPubKey)
	}

	// Setup committee validators.
	inited := c.setupValidators(ownShares)
	if len(inited) == 0 {
		// If no validators were started and therefore we're not subscribed to any subnets,
		// then subscribe to a random subnet to participate in the network.
		if err := c.network.SubscribeRandoms(c.logger, 1); err != nil {
			c.logger.Error("failed to subscribe to random subnets", zap.Error(err))
		}
	}
	close(c.committeeValidatorSetup)

	// Start validators.
	c.startValidators(inited)

	// Fetch metadata for all validators.
	start := time.Now()
	err := beaconprotocol.UpdateValidatorsMetadata(c.logger, allPubKeys, c, c.beacon, c.onMetadataUpdated)
	if err != nil {
		c.logger.Error("failed to update validators metadata after setup",
			zap.Int("shares", len(allPubKeys)),
			fields.Took(time.Since(start)),
			zap.Error(err))
	} else {
		c.logger.Debug("updated validators metadata after setup",
			zap.Int("shares", len(allPubKeys)),
			fields.Took(time.Since(start)))
	}
}

// setupValidators setup and starts validators from the given shares.
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (c *controller) setupValidators(shares []*ssvtypes.SSVShare) []*validator.Validator {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	var validators []*validator.Validator
	for _, validatorShare := range shares {
		var initialized bool
		v, err := c.onShareInit(validatorShare)
		if err != nil {
			c.logger.Warn("could not start validator", fields.PubKey(validatorShare.ValidatorPubKey), zap.Error(err))
			errs = append(errs, err)
		}
		if v != nil {
			initialized = true
		}
		if !initialized && err == nil {
			// Fetch metadata, if needed.
			fetchMetadata = append(fetchMetadata, validatorShare.ValidatorPubKey)
		}
		if initialized {
			validators = append(validators, v)
		}
	}
	c.logger.Info("init validators done", zap.Int("map size", c.validatorsMap.Size()),
		zap.Int("failures", len(errs)), zap.Int("missing_metadata", len(fetchMetadata)),
		zap.Int("shares", len(shares)), zap.Int("initialized", len(validators)))
	return validators
}

func (c *controller) startValidators(validators []*validator.Validator) int {
	var started int
	var errs []error
	for _, v := range validators {
		s, err := c.startValidator(v)
		if err != nil {
			c.logger.Error("could not start validator", zap.Error(err))
			errs = append(errs, err)
			continue
		}
		if s {
			started++
		}
	}

	c.logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.Size()),
		zap.Int("failures", len(errs)),
		zap.Int("shares", len(validators)), zap.Int("started", started))
	return started
}

// setupNonCommitteeValidators trigger SyncHighestDecided for each validator
// to start consensus flow which would save the highest decided instance
// and sync any gaps (in protocol/v2/qbft/controller/decided.go).
func (c *controller) setupNonCommitteeValidators() {
	nonCommitteeShares := c.sharesStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(nonCommitteeShares) == 0 {
		c.logger.Info("could not find non-committee validators")
		return
	}

	pubKeys := make([][]byte, 0, len(nonCommitteeShares))
	for _, validatorShare := range nonCommitteeShares {
		pubKeys = append(pubKeys, validatorShare.ValidatorPubKey)
	}
	if len(pubKeys) > 0 {
		c.logger.Debug("updating metadata for non-committee validators", zap.Int("count", len(pubKeys)))
		if err := beaconprotocol.UpdateValidatorsMetadata(c.logger, pubKeys, c, c.beacon, c.onMetadataUpdated); err != nil {
			c.logger.Warn("could not update all validators", zap.Error(err))
		}
	}
}

// StartNetworkHandlers init msg worker that handles network messages
func (c *controller) StartNetworkHandlers() {
	// first, set stream handlers
	if err := c.setupNetworkHandlers(); err != nil {
		c.logger.Panic("could not register stream handlers", zap.Error(err))
	}
	c.network.UseMessageRouter(c.messageRouter)
	for i := 0; i < networkRouterConcurrency; i++ {
		go c.handleRouterMessages()
	}
	c.messageWorker.UseHandler(c.handleWorkerMessages)
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(pk string, metadata *beaconprotocol.ValidatorMetadata) error {
	c.metadataLastUpdated[pk] = time.Now()

	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	// Save metadata to share storage.
	err := c.sharesStorage.UpdateValidatorMetadata(pk, metadata)
	if err != nil {
		return errors.Wrap(err, "could not update validator metadata")
	}

	// If this validator is not ours or is liquidated, don't start it.
	pkBytes, err := hex.DecodeString(pk)
	if err != nil {
		return errors.Wrap(err, "could not decode public key")
	}
	share := c.sharesStorage.Get(nil, pkBytes)
	if share == nil {
		return errors.New("share was not found")
	}
	if !share.BelongsToOperator(c.operatorDataStore.GetOperatorID()) || share.Liquidated {
		return nil
	}

	// Start validator (if not already started).
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.Share.BeaconMetadata = metadata
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Warn("could not start validator", zap.Error(err))
		}
	} else {
		c.logger.Info("starting new validator", zap.String("pubKey", pk))

		started, err := c.onShareStart(share)
		if err != nil {
			return errors.Wrap(err, "could not start validator")
		}
		if started {
			c.logger.Debug("started share after metadata update", zap.Bool("started", started))
		}
	}
	return nil
}

// GetValidator returns a validator instance from ValidatorsMap
func (c *controller) GetValidator(pubKey string) (*validator.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

func (c *controller) ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) {
	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	pubKeyString := hex.EncodeToString(pk[:])
	if v, ok := c.GetValidator(pubKeyString); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, types.GetDefaultDomain())
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}
		if pushed := v.Queues[duty.Type].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	} else {
		logger.Warn("could not find validator", fields.PubKey(duty.PubKey[:]))
	}
}

// CreateDutyExecuteMsg returns ssvMsg with event type of duty execute
func CreateDutyExecuteMsg(duty *spectypes.Duty, pubKey phase0.BLSPubKey, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	edd, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal execute duty data")
	}
	msg := types.EventMsg{
		Type: types.ExecuteDuty,
		Data: edd,
	}
	data, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode event msg")
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(domain, pubKey[:], duty.Type),
		Data:    data,
	}, nil
}

// CommitteeActiveIndices fetches indices of in-committee validators who are active at the given epoch.
func (c *controller) CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex {
	validators := c.validatorsMap.GetAll()
	indices := make([]phase0.ValidatorIndex, 0, len(validators))
	for _, v := range validators {
		if v.Share.IsAttesting(epoch) {
			indices = append(indices, v.Share.BeaconMetadata.Index)
		}
	}
	return indices
}

func (c *controller) AllActiveIndices(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex {
	if afterInit {
		<-c.committeeValidatorSetup
	}
	shares := c.sharesStorage.List(nil, registrystorage.ByAttesting(epoch))
	indices := make([]phase0.ValidatorIndex, len(shares))
	for i, share := range shares {
		indices[i] = share.BeaconMetadata.Index
	}
	return indices
}

// onMetadataUpdated is called when validator's metadata was updated
func (c *controller) onMetadataUpdated(pk string, meta *beaconprotocol.ValidatorMetadata) {
	if meta == nil {
		return
	}
	logger := c.logger.With(zap.String("pk", pk))

	if v, exist := c.GetValidator(pk); exist {
		// update share object owned by the validator
		// TODO: check if this updates running validators
		if !v.Share.BeaconMetadata.Equals(meta) {
			v.Share.BeaconMetadata.Status = meta.Status
			v.Share.BeaconMetadata.Balance = meta.Balance
			v.Share.BeaconMetadata.ActivationEpoch = meta.ActivationEpoch
			logger.Debug("metadata was updated")
		}
		_, err := c.startValidator(v)
		if err != nil {
			logger.Warn("could not start validator after metadata update",
				zap.String("pk", pk), zap.Error(err), zap.Any("metadata", meta))
		}
		return
	}
}

// onShareStop is called when a validator was removed or liquidated
func (c *controller) onShareStop(pubKey spectypes.ValidatorPK) {
	// remove from ValidatorsMap
	v := c.validatorsMap.RemoveValidator(hex.EncodeToString(pubKey))

	// stop instance
	if v != nil {
		v.Stop()
	}
}

func (c *controller) onShareInit(share *ssvtypes.SSVShare) (*validator.Validator, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		c.logger.Warn("skipping validator until it becomes active", fields.PubKey(share.ValidatorPubKey))
		return nil, nil
	}

	if err := c.setShareFeeRecipient(share, c.recipientsStorage.GetRecipientData); err != nil {
		return nil, fmt.Errorf("could not set share fee recipient: %w", err)
	}

	// Start a committee validator.
	v, found := c.validatorsMap.GetValidator(hex.EncodeToString(share.ValidatorPubKey))
	if !found {
		if !share.HasBeaconMetadata() {
			return nil, fmt.Errorf("beacon metadata is missing")
		}

		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(c.context)

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.DutyRunners = SetupRunners(ctx, c.logger, opts)

		v = validator.NewValidator(ctx, cancel, opts)
		c.validatorsMap.CreateValidator(hex.EncodeToString(share.ValidatorPubKey), v)

		c.printShare(share, "setup validator done")

	} else {
		c.printShare(v.Share, "get validator")
	}

	return v, nil
}

func (c *controller) onShareStart(share *ssvtypes.SSVShare) (bool, error) {
	v, err := c.onShareInit(share)
	if err != nil || v == nil {
		return false, err
	}

	return c.startValidator(v)
}

func (c *controller) printShare(s *ssvtypes.SSVShare, msg string) {
	committee := make([]string, len(s.Committee))
	for i, c := range s.Committee {
		committee[i] = fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.OperatorID, c.PubKey)
	}
	c.logger.Debug(msg,
		fields.PubKey(s.ValidatorPubKey),
		zap.Uint64("node_id", s.OperatorID),
		zap.Strings("committee", committee),
		fields.FeeRecipient(s.FeeRecipientAddress[:]),
	)
}

func (c *controller) setShareFeeRecipient(share *ssvtypes.SSVShare, getRecipientData GetRecipientDataFunc) error {
	data, found, err := getRecipientData(nil, share.OwnerAddress)
	if err != nil {
		return errors.Wrap(err, "could not get recipient data")
	}

	var feeRecipient bellatrix.ExecutionAddress
	if !found {
		c.logger.Debug("setting fee recipient to owner address",
			fields.Validator(share.ValidatorPubKey), fields.FeeRecipient(share.OwnerAddress.Bytes()))
		copy(feeRecipient[:], share.OwnerAddress.Bytes())
	} else {
		c.logger.Debug("setting fee recipient to storage data",
			fields.Validator(share.ValidatorPubKey), fields.FeeRecipient(data.FeeRecipient[:]))
		feeRecipient = data.FeeRecipient
	}
	share.SetFeeRecipient(feeRecipient)

	return nil
}

func (c *controller) validatorStart(validator *validator.Validator) (bool, error) {
	if c.validatorStartFunc == nil {
		return validator.Start(c.logger)
	}
	return c.validatorStartFunc(validator)
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *validator.Validator) (bool, error) {
	c.reportValidatorStatus(v.Share.ValidatorPubKey, v.Share.BeaconMetadata)
	if v.Share.BeaconMetadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	started, err := c.validatorStart(v)
	if err != nil {
		c.metrics.ValidatorError(v.Share.ValidatorPubKey)
		return false, errors.Wrap(err, "could not start validator")
	}
	if started {
		c.recentlyStartedValidators++
	}
	return true, nil
}

// UpdateValidatorMetaDataLoop updates metadata of validators in an interval
func (c *controller) UpdateValidatorMetaDataLoop() {
	var interval = c.beacon.GetBeaconNetwork().SlotDurationSec() * 2

	// Prepare share filters.
	filters := []registrystorage.SharesFilter{}

	// Filter for validators who are not liquidated.
	filters = append(filters, registrystorage.ByNotLiquidated())

	// Filter for validators which haven't been updated recently.
	filters = append(filters, func(s *ssvtypes.SSVShare) bool {
		last, ok := c.metadataLastUpdated[string(s.ValidatorPubKey)]
		return !ok || time.Since(last) > c.metadataUpdateInterval
	})

	for {
		time.Sleep(interval)
		start := time.Now()

		// Get the shares to fetch metadata for.
		shares := c.sharesStorage.List(nil, filters...)
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.ValidatorPubKey)
			c.metadataLastUpdated[string(share.ValidatorPubKey)] = time.Now()
		}

		// TODO: continue if there is nothing to update.

		c.recentlyStartedValidators = 0
		if len(pks) > 0 {
			err := beaconprotocol.UpdateValidatorsMetadata(c.logger, pks, c, c.beacon, c.onMetadataUpdated)
			if err != nil {
				c.logger.Warn("failed to update validators metadata", zap.Error(err))
				continue
			}
		}
		c.logger.Debug("updated validators metadata",
			zap.Int("validators", len(shares)),
			zap.Uint64("started_validators", c.recentlyStartedValidators),
			fields.Took(time.Since(start)))

		// Notify DutyScheduler of new validators.
		if c.recentlyStartedValidators > 0 {
			select {
			case c.indicesChange <- struct{}{}:
			case <-time.After(interval):
				c.logger.Warn("timed out while notifying DutyScheduler of new validators")
			}
		}
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
		spectypes.BNRoleValidatorRegistration,
		spectypes.BNRoleVoluntaryExit,
	}

	domainType := ssvtypes.GetDefaultDomain()
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
			Storage:               options.Storage.Get(role),
			Network:               options.Network,
			Timer:                 roundtimer.New(ctx, options.BeaconNetwork, role, nil),
			SignatureVerification: true,
		}
		config.ValueCheckF = valueCheckF

		identifier := spectypes.NewMsgID(ssvtypes.GetDefaultDomain(), options.SSVShare.Share.ValidatorPubKey, role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], &options.SSVShare.Share, config, options.FullNode)
		qbftCtrl.NewDecidedHandler = options.NewDecidedHandler
		return qbftCtrl
	}

	runners := runner.DutyRunners{}
	for _, role := range runnersType {
		switch role {
		case spectypes.BNRoleAttester:
			valCheck := specssv.AttesterValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.BNRoleAttester, valCheck)
			runners[role] = runner.NewAttesterRunnner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, valCheck, 0)
		case spectypes.BNRoleProposer:
			proposedValueCheck := specssv.ProposerValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.BNRoleProposer, proposedValueCheck)
			runners[role] = runner.NewProposerRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, proposedValueCheck, 0)
		case spectypes.BNRoleAggregator:
			aggregatorValueCheckF := specssv.AggregatorValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleAggregator, aggregatorValueCheckF)
			runners[role] = runner.NewAggregatorRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, aggregatorValueCheckF, 0)
		case spectypes.BNRoleSyncCommittee:
			syncCommitteeValueCheckF := specssv.SyncCommitteeValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleSyncCommittee, syncCommitteeValueCheckF)
			runners[role] = runner.NewSyncCommitteeRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, syncCommitteeValueCheckF, 0)
		case spectypes.BNRoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := specssv.SyncCommitteeContributionValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.BNRoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role] = runner.NewSyncCommitteeAggregatorRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, syncCommitteeContributionValueCheckF, 0)
		case spectypes.BNRoleValidatorRegistration:
			qbftCtrl := buildController(spectypes.BNRoleValidatorRegistration, nil)
			runners[role] = runner.NewValidatorRegistrationRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer)
		case spectypes.BNRoleVoluntaryExit:
			runners[role] = runner.NewVoluntaryExitRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, options.Beacon, options.Network, options.Signer)
		}
	}
	return runners
}
