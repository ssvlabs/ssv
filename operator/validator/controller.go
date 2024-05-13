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
	"github.com/ethereum/go-ethereum/common"
	"github.com/jellydator/ttlcache/v3"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/networkconfig"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validators"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
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
	BuilderProposals           bool `yaml:"BuilderProposals" env:"BUILDER_PROPOSALS" env-default:"false" env-description:"Use external builders to produce blocks"`
	BeaconSigner               spectypes.BeaconSigner
	OperatorSigner             spectypes.OperatorSigner
	OperatorDataStore          operatordatastore.OperatorDataStore
	RegistryStorage            nodestorage.Storage
	RecipientsStorage          Recipients
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole
	StorageMap                 *storage.QBFTStores
	Metrics                    validator.Metrics
	MessageValidator           validation.MessageValidator
	ValidatorsMap              *validators.ValidatorsMap
	NetworkConfig              networkconfig.NetworkConfig

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
	GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool)
	ExecuteDuty(logger *zap.Logger, duty *spectypes.BeaconDuty)
	ExecuteCommitteeDuty(logger *zap.Logger, committeeID spectypes.ClusterID, duty *spectypes.CommitteeDuty)
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
	UpdateValidatorMetadata(pk spectypes.ValidatorPK, metadata *beaconprotocol.ValidatorMetadata) error
}

type P2PNetwork interface {
	protocolp2p.Broadcaster
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

	networkConfig     networkconfig.NetworkConfig
	sharesStorage     SharesStorage
	operatorsStorage  registrystorage.Operators
	recipientsStorage Recipients
	ibftStorageMap    *storage.QBFTStores

	beacon         beaconprotocol.BeaconNode
	beaconSigner   spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner

	operatorDataStore operatordatastore.OperatorDataStore

	validatorOptions        validator.Options
	validatorsMap           *validators.ValidatorsMap
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
	recentlyStartedCommittees uint64
	metadataLastUpdated       map[spectypes.ValidatorPK]time.Time
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

	sigVerifier := validator.NewSignatureVerifier()

	validatorOptions := validator.Options{ //TODO add vars
		NetworkConfig: options.NetworkConfig,
		Network:       options.Network,
		Beacon:        options.Beacon,
		BeaconNetwork: options.BeaconNetwork.GetNetwork(),
		Storage:       options.StorageMap,
		//Share:   nil,  // set per validator
		Signer:            options.BeaconSigner,
		OperatorSigner:    options.OperatorSigner,
		SignatureVerifier: sigVerifier,
		//Mode: validator.ModeRW // set per validator
		DutyRunners:       nil, // set per validator
		NewDecidedHandler: options.NewDecidedHandler,
		FullNode:          options.FullNode,
		Exporter:          options.Exporter,
		BuilderProposals:  options.BuilderProposals,
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
		networkConfig:     options.NetworkConfig,
		sharesStorage:     options.RegistryStorage.Shares(),
		operatorsStorage:  options.RegistryStorage,
		recipientsStorage: options.RegistryStorage,
		ibftStorageMap:    options.StorageMap,
		context:           options.Context,
		beacon:            options.Beacon,
		operatorDataStore: options.OperatorDataStore,
		beaconSigner:      options.BeaconSigner,
		operatorSigner:    options.OperatorSigner,
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
		metadataLastUpdated:     make(map[spectypes.ValidatorPK]time.Time),
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

			// TODO: only try copying clusterid if validator failed
			pk := msg.GetID().GetSenderID()
			var cid spectypes.ClusterID
			copy(cid[:], pk[16:])

			if v, ok := c.validatorsMap.GetValidator(spectypes.ValidatorPK(pk)); ok {
				v.HandleMessage(c.logger, msg)
			} else if vc, ok := c.validatorsMap.GetCommittee(cid); ok {
				vc.HandleMessage(c.logger, msg)
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

var nonCommitteeValidatorTTLs = map[spectypes.RunnerRole]phase0.Slot{
	spectypes.RoleCommittee:  64,
	spectypes.RoleProposer:   4,
	spectypes.RoleAggregator: 4,
	//spectypes.BNRoleSyncCommittee:             4,
	spectypes.RoleSyncCommitteeContribution: 4,
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
			share := c.sharesStorage.Get(nil, msg.GetID().GetSenderID())
			if share == nil {
				return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetID().GetSenderID()))
			}

			opts := c.validatorOptions
			opts.SSVShare = share
			operator, err := c.operatorFromShare(opts.SSVShare)
			if err != nil {
				return err
			}
			opts.Operator = operator

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
		allPubKeys = append(allPubKeys, share.ValidatorPubKey[:])
	}

	// Setup committee validators.
	inited, committees := c.setupValidators(ownShares)
	if len(inited) == 0 {
		// If no validators were started and therefore we're not subscribed to any subnets,
		// then subscribe to a random subnet to participate in the network.
		if err := c.network.SubscribeRandoms(c.logger, 1); err != nil {
			c.logger.Error("failed to subscribe to random subnets", zap.Error(err))
		}
	}
	close(c.committeeValidatorSetup)

	// Start validators.
	c.startValidators(inited, committees)

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
func (c *controller) setupValidators(shares []*ssvtypes.SSVShare) ([]*validator.Validator, []*validator.Committee) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	var validators []*validator.Validator
	var committees []*validator.Committee
	for _, validatorShare := range shares {
		var initialized bool
		v, vc, err := c.onShareInit(validatorShare)
		if err != nil {
			c.logger.Warn("could not start validator", fields.PubKey(validatorShare.ValidatorPubKey[:]), zap.Error(err))
			errs = append(errs, err)
		}
		if v != nil {
			initialized = true
		}
		if !initialized && err == nil {
			// Fetch metadata, if needed.
			fetchMetadata = append(fetchMetadata, validatorShare.ValidatorPubKey[:])
		}
		if initialized {
			validators = append(validators, v)
			committees = append(committees, vc)
		}
	}
	c.logger.Info("init validators done", zap.Int("validators_size", c.validatorsMap.SizeValidators()), zap.Int("committee_size", c.validatorsMap.SizeCommittees()),
		zap.Int("failures", len(errs)), zap.Int("missing_metadata", len(fetchMetadata)),
		zap.Int("shares", len(shares)), zap.Int("initialized", len(validators)))
	return validators, committees
}

func (c *controller) startValidators(validators []*validator.Validator, committees []*validator.Committee) int {
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

	for _, vc := range committees {
		s, err := c.startCommittee(vc)
		if err != nil {
			c.logger.Error("could not start committee", zap.Error(err))
			errs = append(errs, err)
			continue
		}
		if s {
			started++
		}
	}

	c.logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.SizeValidators()),
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
		pubKeys = append(pubKeys, validatorShare.ValidatorPubKey[:])
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
func (c *controller) UpdateValidatorMetadata(pk spectypes.ValidatorPK, metadata *beaconprotocol.ValidatorMetadata) error {
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
	share := c.sharesStorage.Get(nil, pk[:])
	if share == nil {
		return errors.New("share was not found")
	}
	if !share.BelongsToOperator(c.operatorDataStore.GetOperatorID()) || share.Liquidated {
		return nil
	}

	// Start validator (if not already started).
	// TODO: why its in the map if not started?
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.Share.BeaconMetadata = metadata
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Warn("could not start validator", zap.Error(err))
		}
		vc, found := c.validatorsMap.GetCommittee(v.Share.CommitteeID())
		if found {
			vc.AddShare(&v.Share.Share)
			_, err := c.startCommittee(vc)
			if err != nil {
				c.logger.Warn("could not start committee", zap.Error(err))
			}
		}
	} else {
		c.logger.Info("starting new validator", fields.PubKey(pk[:]))

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
func (c *controller) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

func (c *controller) ExecuteDuty(logger *zap.Logger, duty *spectypes.BeaconDuty) {
	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	pk := make([]byte, 48)
	copy(pk, duty.PubKey[:])

	if v, ok := c.GetValidator(spectypes.ValidatorPK(pk)); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, c.networkConfig.Domain)
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}

		if pushed := v.Queues[duty.RunnerRole()].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	} else {
		logger.Warn("could not find validator")
	}
}

func (c *controller) ExecuteCommitteeDuty(logger *zap.Logger, committeeID spectypes.ClusterID, duty *spectypes.CommitteeDuty) {
	logger = logger.With(fields.Slot(duty.Slot), fields.Role(duty.RunnerRole()))

	if cm, ok := c.validatorsMap.GetCommittee(committeeID); ok {
		ssvMsg, err := CreateCommitteeDutyExecuteMsg(duty, committeeID, c.networkConfig.Domain)
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}
		// TODO alan: no queue in cc, what should we do?
		if err := cm.OnExecuteDuty(logger, dec.Body.(*types.EventMsg)); err != nil {
			logger.Error("could not execute committee duty", zap.Error(err))
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	} else {
		logger.Warn("could not find committee", fields.CommitteeID(committeeID))
	}
}

// CreateDutyExecuteMsg returns ssvMsg with event type of execute duty
func CreateDutyExecuteMsg(duty *spectypes.BeaconDuty, pubKey []byte, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	data, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, pubKey, duty.RunnerRole(), data)
}

// CreateCommitteeDutyExecuteMsg returns ssvMsg with event type of execute committee duty
func CreateCommitteeDutyExecuteMsg(duty *spectypes.CommitteeDuty, committeeID spectypes.ClusterID, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeCommitteeDutyData := types.ExecuteCommitteeDutyData{Duty: duty}
	data, err := json.Marshal(executeCommitteeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute committee duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, committeeID[:], spectypes.RoleCommittee, data)
}

func dutyDataToSSVMsg(
	domain spectypes.DomainType,
	msgIdentifier []byte,
	runnerRole spectypes.RunnerRole,
	data []byte,
) (*spectypes.SSVMessage, error) {
	msg := types.EventMsg{
		Type: ssvtypes.ExecuteDuty,
		Data: data,
	}
	msgData, err := msg.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode event msg: %w", err)
	}

	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(domain, msgIdentifier, runnerRole),
		Data:    msgData,
	}, nil
}

// CommitteeActiveIndices fetches indices of in-committee validators who are active at the given epoch.
func (c *controller) CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex {
	vals := c.validatorsMap.GetAllValidators()
	indices := make([]phase0.ValidatorIndex, 0, len(vals))
	for _, v := range vals {
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
func (c *controller) onMetadataUpdated(pk spectypes.ValidatorPK, meta *beaconprotocol.ValidatorMetadata) {
	if meta == nil {
		return
	}
	logger := c.logger.With(fields.PubKey(pk[:]))

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
				zap.Error(err), zap.Any("metadata", meta))
		}
		if vc, vcexist := c.validatorsMap.GetCommittee(v.Share.CommitteeID()); vcexist {
			vc.AddShare(&v.Share.Share)
			_, err := c.startCommittee(vc)
			if err != nil {
				logger.Warn("could not start committee after metadata update",
					zap.Error(err), zap.Any("metadata", meta))
			}
		}
		return
	}
}

// onShareStop is called when a validator was removed or liquidated
func (c *controller) onShareStop(pubKey spectypes.ValidatorPK) {
	// remove from ValidatorsMap
	v := c.validatorsMap.RemoveValidator(pubKey)

	// stop instance
	if v != nil {
		v.Stop()
		c.logger.Debug("validator was stopped", fields.PubKey(pubKey[:]))
	}

	vc, ok := c.validatorsMap.GetCommittee(v.Share.CommitteeID())
	if ok {
		vc.RemoveShare(v.Share.Share.ValidatorIndex)
		// TODO: (Alan) remove committee if last share and stop
	}
}

// todo wrapper to start both validator and committee
type starter interface {
	Start() error
}

func (c *controller) onShareInit(share *ssvtypes.SSVShare) (*validator.Validator, *validator.Committee, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		c.logger.Warn("skipping validator until it becomes active", fields.PubKey(share.ValidatorPubKey[:]))
		return nil, nil, nil
	}

	if err := c.setShareFeeRecipient(share, c.recipientsStorage.GetRecipientData); err != nil {
		return nil, nil, fmt.Errorf("could not set share fee recipient: %w", err)
	}

	operator, err := c.operatorFromShare(share)
	if err != nil {
		return nil, nil, err
	}

	// Start a committee validator.
	v, found := c.validatorsMap.GetValidator(share.ValidatorPubKey)
	if !found {
		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(c.context)

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.Operator = operator
		opts.DutyRunners = SetupRunners(ctx, c.logger, opts)

		v = validator.NewValidator(ctx, cancel, opts)
		c.validatorsMap.PutValidator(share.ValidatorPubKey, v)

		c.printShare(share, "setup validator done")

	}

	// Start a committee validator.
	vc, found := c.validatorsMap.GetCommittee(operator.ClusterID)
	if !found {
		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, _ := context.WithCancel(c.context)

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.Operator = operator

		logger := c.logger.With([]zap.Field{
			zap.String("committee", fields.FormatCommittee(operator.Committee)),
			zap.String("committee_id", hex.EncodeToString(operator.ClusterID[:])),
		}...)

		committeeRunnerFunc := SetupCommitteeRunners(ctx, c.networkConfig, opts)

		vc = validator.NewCommittee(c.context, logger, c.beacon.GetBeaconNetwork(), operator, opts.SignatureVerifier, committeeRunnerFunc)
		vc.AddShare(&share.Share)
		c.validatorsMap.PutCommittee(operator.ClusterID, vc)

		c.printShare(share, "setup committee done")

	} else {
		vc.AddShare(&share.Share)
		c.printShare(v.Share, "added share to committee")
	}

	return v, vc, nil
}

func (c *controller) operatorFromShare(share *ssvtypes.SSVShare) (*spectypes.Operator, error) {

	committeemembers := make([]*spectypes.CommitteeMember, len(share.Committee))

	for i, cm := range share.Committee {
		opdata, found, err := c.operatorsStorage.GetOperatorData(nil, cm.Signer)
		if err != nil {
			return nil, fmt.Errorf("could not get operator data: %w", err)
		}
		if !found {
			//TODO alan: support removed ops
			return nil, fmt.Errorf("operator not found")
		}
		committeemembers[i] = &spectypes.CommitteeMember{
			OperatorID:        cm.Signer,
			SSVOperatorPubKey: opdata.PublicKey,
		}
	}

	q, pc := types.ComputeQuorumAndPartialQuorum(len(share.Committee))

	return &spectypes.Operator{
		OperatorID:        c.operatorDataStore.GetOperatorID(),
		ClusterID:         share.CommitteeID(),
		SSVOperatorPubKey: c.operatorDataStore.GetOperatorData().PublicKey,
		Quorum:            q,
		PartialQuorum:     pc,
		Committee:         committeemembers,
	}, nil
}

func (c *controller) onShareStart(share *ssvtypes.SSVShare) (bool, error) {
	v, vc, err := c.onShareInit(share)
	if err != nil || v == nil {
		return false, err
	}

	started, err := c.startValidator(v)
	if err != nil {
		return false, err
	}
	vcstarted, err := c.startCommittee(vc)
	if err != nil {
		return false, err
	}
	return started && vcstarted, nil
}

func (c *controller) printShare(s *ssvtypes.SSVShare, msg string) {
	committee := make([]string, len(s.Committee))
	for i, c := range s.Committee {
		committee[i] = fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.Signer, c.SharePubKey)
	}
	c.logger.Debug(msg,
		fields.PubKey(s.ValidatorPubKey[:]),
		zap.Bool("own_validator", s.BelongsToOperator(c.operatorDataStore.GetOperatorID())),
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
			fields.Validator(share.ValidatorPubKey[:]), fields.FeeRecipient(share.OwnerAddress.Bytes()))
		copy(feeRecipient[:], share.OwnerAddress.Bytes())
	} else {
		c.logger.Debug("setting fee recipient to storage data",
			fields.Validator(share.ValidatorPubKey[:]), fields.FeeRecipient(data.FeeRecipient[:]))
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

//func (c *controller) startValidatorAndCommittee(v *val)

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *validator.Validator) (bool, error) {
	c.reportValidatorStatus(v.Share.ValidatorPubKey[:], v.Share.BeaconMetadata)
	if v.Share.BeaconMetadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	started, err := c.validatorStart(v)
	if err != nil {
		c.metrics.ValidatorError(v.Share.ValidatorPubKey[:])
		return false, errors.Wrap(err, "could not start validator")
	}
	if started {
		c.recentlyStartedValidators++
	}

	return true, nil
}

func (c *controller) startCommittee(vc *validator.Committee) (bool, error) {
	//TODO alan: currently nothing to start in committee?
	// c.logger.Debug("committee started ", zap.String("committee_id", hex.EncodeToString(vc.Operator.ClusterID[:])))
	//cstarted, err := vc.Start() // TODO alan : make it testable
	//if err != nil {
	//	// todo alan: metrics
	//	//c.metrics.ValidatorError(vc.Share.ValidatorPubKey[:])
	//	return false, errors.Wrap(err, "could not start committee")
	//}
	//if cstarted {
	//	c.recentlyStartedCommittees++
	//}

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
		last, ok := c.metadataLastUpdated[s.ValidatorPubKey]
		return !ok || time.Since(last) > c.metadataUpdateInterval
	})

	for {
		time.Sleep(interval)
		start := time.Now()

		// Get the shares to fetch metadata for.
		shares := c.sharesStorage.List(nil, filters...)
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.ValidatorPubKey[:])
			c.metadataLastUpdated[share.ValidatorPubKey] = time.Now()
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

// TODO alan: use spec when they fix bugs
func TempBeaconVoteValueCheckF(
	signer spectypes.BeaconSigner,
	slot phase0.Slot,
	sharePublicKey []byte,
	estimatedCurrentEpoch phase0.Epoch,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		bv := spectypes.BeaconVote{}
		if err := bv.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding beacon vote")
		}

		if bv.Target.Epoch > estimatedCurrentEpoch+1 {
			return errors.New("attestation data target epoch is into far future")
		}

		if bv.Source.Epoch >= bv.Target.Epoch {
			return errors.New("attestation data source > target")
		}

		// attestationData := &phase0.AttestationData{
		// 	Slot: slot,
		// 	// CommitteeIndex doesn't matter for slashing checks
		// 	Index:           0,
		// 	BeaconBlockRoot: bv.BlockRoot,
		// 	Source:          bv.Source,
		// 	Target:          bv.Target,
		// }

		// TODO: (Alan) REVERT SLASHING CHECK
		// return signer.IsAttestationSlashable(sharePublicKey, attestationData)
		return nil
	}
}

func SetupCommitteeRunners(
	ctx context.Context,
	networkConfig networkconfig.NetworkConfig,
	options validator.Options,
) func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner {
	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner:      options.Signer,
			OperatorSigner:    options.OperatorSigner,
			SigningPK:         options.SSVShare.ValidatorPubKey[:], // TODO right val?
			SignatureVerifier: options.SignatureVerifier,
			Domain:            networkConfig.Domain,
			ValueCheckF:       nil, // sets per role type
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

		identifier := spectypes.NewMsgID(networkConfig.Domain, options.Operator.ClusterID[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.FullNode)
		qbftCtrl.NewDecidedHandler = options.NewDecidedHandler
		return qbftCtrl
	}

	return func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner {
		// Create a committee runner.
		epoch := options.BeaconNetwork.GetBeaconNetwork().EstimatedEpochAtSlot(slot)
		valCheck := TempBeaconVoteValueCheckF(options.Signer, slot, options.SSVShare.Share.SharePubKey, epoch) // TODO: (Alan) fix slashing check (committee is not 1 pubkey)
		crunner := runner.NewCommitteeRunner(
			networkConfig,
			options.BeaconNetwork.GetBeaconNetwork(),
			shares,
			buildController(spectypes.RoleCommittee, valCheck),
			options.Beacon,
			options.Network,
			options.Signer,
			options.OperatorSigner,
			valCheck,
		)
		return crunner.(*runner.CommitteeRunner)
	}
}

// SetupRunners initializes duty runners for the given validator
func SetupRunners(
	ctx context.Context,
	logger *zap.Logger,
	options validator.Options,
) runner.ValidatorDutyRunners {
	if options.SSVShare == nil || options.SSVShare.BeaconMetadata == nil {
		logger.Error("missing validator metadata", zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey[:])))
		return runner.ValidatorDutyRunners{} // TODO need to find better way to fix it
	}

	runnersType := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleProposer,
		spectypes.RoleAggregator,
		spectypes.RoleSyncCommitteeContribution,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit,
	}

	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner:   options.Signer,
			OperatorSigner: options.OperatorSigner,
			SigningPK:      options.SSVShare.ValidatorPubKey[:], // TODO right val?
			Domain:         options.NetworkConfig.Domain,
			ValueCheckF:    nil, // sets per role type
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

		identifier := spectypes.NewMsgID(options.NetworkConfig.Domain, options.SSVShare.Share.ValidatorPubKey[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.FullNode)
		qbftCtrl.NewDecidedHandler = options.NewDecidedHandler
		return qbftCtrl
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share) // TODO: fill the map
	shareMap[options.SSVShare.ValidatorIndex] = &options.SSVShare.Share

	runners := runner.ValidatorDutyRunners{}
	for _, role := range runnersType {
		switch role {
		//case spectypes.BNRoleAttester:
		//	valCheck := specssv.AttesterValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
		//	qbftCtrl := buildController(spectypes.BNRoleAttester, valCheck)
		//	runners[role] = runner.NewAttesterRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, valCheck, 0)
		case spectypes.RoleProposer:
			proposedValueCheck := specssv.ProposerValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.RoleProposer, proposedValueCheck)
			runners[role] = runner.NewProposerRunner(options.BeaconNetwork.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, proposedValueCheck, 0)
			runners[role].(*runner.ProposerRunner).ProducesBlindedBlocks = options.BuilderProposals // apply blinded block flag
		case spectypes.RoleAggregator:
			aggregatorValueCheckF := specssv.AggregatorValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.RoleAggregator, aggregatorValueCheckF)
			runners[role] = runner.NewAggregatorRunner(options.BeaconNetwork.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, aggregatorValueCheckF, 0)
		//case spectypes.BNRoleSyncCommittee:
		//syncCommitteeValueCheckF := specssv.SyncCommitteeValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
		//qbftCtrl := buildController(spectypes.BNRoleSyncCommittee, syncCommitteeValueCheckF)
		//runners[role] = runner.NewSyncCommitteeRunner(options.BeaconNetwork.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeValueCheckF, 0)
		case spectypes.RoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := specssv.SyncCommitteeContributionValueCheckF(options.Signer, options.BeaconNetwork.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.RoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role] = runner.NewSyncCommitteeAggregatorRunner(options.BeaconNetwork.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeContributionValueCheckF, 0)
		case spectypes.RoleValidatorRegistration:
			qbftCtrl := buildController(spectypes.RoleValidatorRegistration, nil)
			runners[role] = runner.NewValidatorRegistrationRunner(options.BeaconNetwork.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		case spectypes.RoleVoluntaryExit:
			runners[role] = runner.NewVoluntaryExitRunner(options.BeaconNetwork.GetBeaconNetwork(), shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		}
	}
	return runners
}
