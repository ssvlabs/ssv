package validator

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/genesisstorage"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validators"
	genesisbeaconprotocol "github.com/ssvlabs/ssv/protocol/genesis/blockchain/beacon"
	genesismessage "github.com/ssvlabs/ssv/protocol/genesis/message"
	genesisqbft "github.com/ssvlabs/ssv/protocol/genesis/qbft"
	genesisqbftcontroller "github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	genesisroundtimer "github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/genesisqueue"
	genesisrunner "github.com/ssvlabs/ssv/protocol/genesis/ssv/runner"
	genesisvalidator "github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	genesisssvtypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/queue/worker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
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
	GenesisBeacon              genesisbeaconprotocol.BeaconNode
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Save decided history rather than just highest messages"`
	Exporter                   bool `yaml:"Exporter" env:"EXPORTER" env-default:"false" env-description:""`
	BeaconSigner               spectypes.BeaconSigner
	OperatorSigner             ssvtypes.OperatorSigner
	OperatorDataStore          operatordatastore.OperatorDataStore
	RegistryStorage            nodestorage.Storage
	RecipientsStorage          Recipients
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole
	StorageMap                 *storage.QBFTStores
	ValidatorStore             registrystorage.ValidatorStore
	Metrics                    validator.Metrics
	MessageValidator           validation.MessageValidator
	ValidatorsMap              *validators.ValidatorsMap
	NetworkConfig              networkconfig.NetworkConfig
	Graffiti                   []byte

	// worker flags
	WorkersCount    int `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"256" env-description:"Number of goroutines to use for message workers"`
	QueueBufferSize int `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"65536" env-description:"Buffer size for message workers"`
	GasLimit        uint64

	// Genesis Flags
	GenesisControllerOptions
}

type GenesisControllerOptions struct {
	Network           genesisspecqbft.Network
	KeyManager        genesisspectypes.KeyManager
	StorageMap        *genesisstorage.QBFTStores
	NewDecidedHandler genesisqbftcontroller.NewDecidedHandler
	Metrics           genesisvalidator.Metrics
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	StartValidators()
	CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	AllActiveIndices(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex
	GetValidator(pubKey spectypes.ValidatorPK) (*validators.ValidatorContainer, bool)
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

	duties.DutyExecutor
}

type committeeObserver struct {
	*validator.CommitteeObserver
	sync.Mutex
}

type Nonce uint16

type Recipients interface {
	GetRecipientData(r basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error)
}

type SharesStorage interface {
	Get(txn basedb.Reader, pubKey []byte) *ssvtypes.SSVShare
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
	UpdateValidatorMetadata(pk spectypes.ValidatorPK, metadata *beaconprotocol.ValidatorMetadata) error
	UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata) error
}

type P2PNetwork interface {
	protocolp2p.Broadcaster
	UseMessageRouter(router network.MessageRouter)
	SubscribeRandoms(logger *zap.Logger, numSubnets int) error
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
	operatorSigner ssvtypes.OperatorSigner

	operatorDataStore operatordatastore.OperatorDataStore

	validatorOptions        validator.Options
	genesisValidatorOptions genesisvalidator.Options
	validatorStore          registrystorage.ValidatorStore
	validatorsMap           *validators.ValidatorsMap
	validatorStartFunc      func(validator *validators.ValidatorContainer) (bool, error)
	committeeValidatorSetup chan struct{}

	metadataUpdateInterval time.Duration

	operatorsIDs         *sync.Map
	network              P2PNetwork
	messageRouter        *messageRouter
	messageWorker        *worker.Worker
	historySyncBatchSize int
	messageValidator     validation.MessageValidator

	// nonCommittees is a cache of initialized committeeObserver instances
	committeesObservers      *ttlcache.Cache[spectypes.MessageID, *committeeObserver]
	committeesObserversMutex sync.Mutex

	recentlyStartedValidators uint64
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

	validatorOptions := validator.Options{ //TODO add vars
		NetworkConfig: options.NetworkConfig,
		Network:       options.Network,
		Beacon:        options.Beacon,
		GenesisBeacon: options.GenesisBeacon,
		Storage:       options.StorageMap,
		//Share:   nil,  // set per validator
		Signer:            options.BeaconSigner,
		OperatorSigner:    options.OperatorSigner,
		DutyRunners:       nil, // set per validator
		NewDecidedHandler: options.NewDecidedHandler,
		FullNode:          options.FullNode,
		Exporter:          options.Exporter,
		GasLimit:          options.GasLimit,
		MessageValidator:  options.MessageValidator,
		Metrics:           options.Metrics,
		Graffiti:          options.Graffiti,
		GenesisOptions: validator.GenesisOptions{
			Network:           options.GenesisControllerOptions.Network,
			Signer:            options.GenesisControllerOptions.KeyManager,
			Storage:           options.GenesisControllerOptions.StorageMap,
			NewDecidedHandler: options.GenesisControllerOptions.NewDecidedHandler,
		},
	}

	//TODO
	genesisValidatorOptions := genesisvalidator.Options{
		Network:       options.GenesisControllerOptions.Network,
		BeaconNetwork: options.NetworkConfig.Beacon,
		Storage:       options.GenesisControllerOptions.StorageMap,
		// SSVShare:   nil,  // set per validator
		Signer:            options.GenesisControllerOptions.KeyManager,
		DutyRunners:       nil, // set per validator
		NewDecidedHandler: options.GenesisControllerOptions.NewDecidedHandler,
		FullNode:          options.FullNode,
		Exporter:          options.Exporter,
		GasLimit:          options.GasLimit,
		MessageValidator:  options.MessageValidator,
		Metrics:           options.GenesisControllerOptions.Metrics,
	}

	// If full node, increase queue size to make enough room
	// for history sync batches to be pushed whole.
	if options.FullNode {
		size := options.HistorySyncBatchSize * 2
		if size > validator.DefaultQueueSize {
			validatorOptions.QueueSize = size
		}
		if size > genesisvalidator.DefaultQueueSize {
			genesisValidatorOptions.QueueSize = size
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
		validatorStore:    options.ValidatorStore,
		context:           options.Context,
		beacon:            options.Beacon,
		operatorDataStore: options.OperatorDataStore,
		beaconSigner:      options.BeaconSigner,
		operatorSigner:    options.OperatorSigner,
		network:           options.Network,

		validatorsMap:           options.ValidatorsMap,
		validatorOptions:        validatorOptions,
		genesisValidatorOptions: genesisValidatorOptions,

		metadataUpdateInterval: options.MetadataUpdateInterval,

		operatorsIDs: operatorsIDs,

		messageRouter:        newMessageRouter(logger),
		messageWorker:        worker.NewWorker(logger, workerCfg),
		historySyncBatchSize: options.HistorySyncBatchSize,

		committeesObservers: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *committeeObserver](time.Minute * 13),
		),
		metadataLastUpdated:     make(map[spectypes.ValidatorPK]time.Time),
		indicesChange:           make(chan struct{}),
		validatorExitCh:         make(chan duties.ExitDescriptor),
		committeeValidatorSetup: make(chan struct{}, 1),

		messageValidator: options.MessageValidator,
	}

	// Start automatic expired item deletion in nonCommitteeValidators.
	go ctrl.committeesObservers.Start()

	return &ctrl
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
			switch m := msg.(type) {
			case *genesisqueue.GenesisSSVMessage:
				if m.MsgType == genesismessage.SSVEventMsgType {
					continue
				}

				pk := m.GetID().GetPubKey()
				if v, ok := c.validatorsMap.GetValidator(spectypes.ValidatorPK(pk[:])); ok {
					v.GenesisValidator.HandleMessage(c.logger, m)
				}
				// TODO: (Alan) make exporter work with genesis message as well
				// } else if c.validatorOptions.Exporter {
				// 	if m.MsgType != genesisspectypes.SSVConsensusMsgType {
				// 		continue // not supporting other types
				// 	}
				// 	if !c.messageWorker.TryEnqueue(m) { // start to save non committee decided messages only post fork
				// 		c.logger.Warn("Failed to enqueue post consensus message: buffer is full")
				// 	}

			case *queue.SSVMessage:
				if m.MsgType == message.SSVEventMsgType {
					continue
				}

				// TODO: only try copying clusterid if validator failed
				dutyExecutorID := m.GetID().GetDutyExecutorID()
				var cid spectypes.CommitteeID
				copy(cid[:], dutyExecutorID[16:])

				if v, ok := c.validatorsMap.GetValidator(spectypes.ValidatorPK(dutyExecutorID)); ok {
					v.Validator.HandleMessage(c.logger, m)
				} else if vc, ok := c.validatorsMap.GetCommittee(cid); ok {
					vc.HandleMessage(c.logger, m)
				} else if c.validatorOptions.Exporter {
					if m.MsgType != spectypes.SSVConsensusMsgType && m.MsgType != spectypes.SSVPartialSignatureMsgType {
						continue
					}
					if !c.messageWorker.TryEnqueue(m) { // start to save non committee decided messages only post fork
						c.logger.Warn("Failed to enqueue post consensus message: buffer is full")
					}
				}

			default:
				// This should be impossible because the channel is typed.
				c.logger.Fatal("unknown message type from router", zap.Any("message", m))
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

func (c *controller) handleWorkerMessages(msg network.DecodedSSVMessage) error {
	var ncv *committeeObserver
	ssvMsg := msg.(*queue.SSVMessage)

	item := c.getNonCommitteeValidators(ssvMsg.GetID())
	if item == nil {
		committeeObserverOptions := validator.CommitteeObserverOptions{
			Logger:            c.logger,
			NetworkConfig:     c.networkConfig,
			ValidatorStore:    c.validatorStore,
			Network:           c.validatorOptions.Network,
			Storage:           c.validatorOptions.Storage,
			FullNode:          c.validatorOptions.FullNode,
			Operator:          c.validatorOptions.Operator,
			OperatorSigner:    c.validatorOptions.OperatorSigner,
			NewDecidedHandler: c.validatorOptions.NewDecidedHandler,
		}
		ncv = &committeeObserver{
			CommitteeObserver: validator.NewCommitteeObserver(convert.MessageID(ssvMsg.MsgID), committeeObserverOptions),
		}
		ttlSlots := nonCommitteeValidatorTTLs[ssvMsg.MsgID.GetRoleType()]
		c.committeesObservers.Set(
			ssvMsg.GetID(),
			ncv,
			time.Duration(ttlSlots)*c.beacon.GetBeaconNetwork().SlotDurationSec(),
		)
	} else {
		ncv = item
	}
	if err := c.handleNonCommitteeMessages(ssvMsg, ncv); err != nil {
		return err
	}
	return nil
}

func (c *controller) handleNonCommitteeMessages(msg *queue.SSVMessage, ncv *committeeObserver) error {
	c.committeesObserversMutex.Lock()
	defer c.committeesObserversMutex.Unlock()

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		// Process proposal messages for committee consensus only to get the roots
		if msg.MsgID.GetRoleType() != spectypes.RoleCommittee {
			return nil
		}

		subMsg, ok := msg.Body.(*specqbft.Message)
		if !ok || subMsg.MsgType != specqbft.ProposalMsgType {
			return nil
		}

		return ncv.OnProposalMsg(msg)
	} else if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return err
		}

		return ncv.ProcessMessage(msg)
	}
	return nil
}

func (c *controller) getNonCommitteeValidators(messageId spectypes.MessageID) *committeeObserver {
	item := c.committeesObservers.Get(messageId)
	if item != nil {
		return item.Value()
	}
	return nil
}

// StartValidators loads all persisted shares and setup the corresponding validators
func (c *controller) StartValidators() {
	// Load non-liquidated shares.
	shares := c.sharesStorage.List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		close(c.committeeValidatorSetup)
		c.logger.Info("could not find validators")
		return
	}

	var ownShares []*ssvtypes.SSVShare
	var allPubKeys = make([][]byte, 0, len(shares))
	for _, share := range shares {
		if c.operatorDataStore.GetOperatorID() != 0 && share.BelongsToOperator(c.operatorDataStore.GetOperatorID()) {
			ownShares = append(ownShares, share)
		}
		allPubKeys = append(allPubKeys, share.ValidatorPubKey[:])
	}

	if c.validatorOptions.Exporter {
		// There are no committee validators to setup.
		close(c.committeeValidatorSetup)
	} else {
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
	}

	// Fetch metadata now if there is none. Otherwise, UpdateValidatorsMetadataLoop will handle it.
	var hasMetadata bool
	for _, share := range shares {
		if !share.Liquidated && share.HasBeaconMetadata() {
			hasMetadata = true
			break
		}
	}
	if !hasMetadata {
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
}

// setupValidators setup and starts validators from the given shares.
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (c *controller) setupValidators(shares []*ssvtypes.SSVShare) ([]*validators.ValidatorContainer, []*validator.Committee) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	var validators []*validators.ValidatorContainer
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

func (c *controller) startValidators(validators []*validators.ValidatorContainer, committees []*validator.Committee) int {
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

// StartNetworkHandlers init msg worker that handles network messages
func (c *controller) StartNetworkHandlers() {
	c.network.UseMessageRouter(c.messageRouter)
	for i := 0; i < networkRouterConcurrency; i++ {
		go c.handleRouterMessages()
	}
	c.messageWorker.UseHandler(c.handleWorkerMessages)
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(pk spectypes.ValidatorPK, metadata *beaconprotocol.ValidatorMetadata) error {
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
		v.UpdateShare(
			func(s *types.SSVShare) {
				s.BeaconMetadata = share.BeaconMetadata
				s.ValidatorIndex = share.ValidatorIndex
			}, func(s *genesistypes.SSVShare) {
				s.BeaconMetadata = share.BeaconMetadata
			},
		)
		_, err := c.startValidator(v)
		if err != nil {
			c.logger.Warn("could not start validator", zap.Error(err))
		}
		vc, found := c.validatorsMap.GetCommittee(v.Share().CommitteeID())
		if found {
			vc.AddShare(&v.Share().Share)
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

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorsMetadata(data map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata) error {
	// TODO: better log and err handling
	for pk, metadata := range data {
		c.metadataLastUpdated[pk] = time.Now()

		if metadata == nil {
			delete(data, pk)
		}
	}

	startdb := time.Now()
	// Save metadata to share storage.
	err := c.sharesStorage.UpdateValidatorsMetadata(data)
	if err != nil {
		return errors.Wrap(err, "could not update validator metadata")
	}
	c.logger.Debug("🆕 updated validators metadata in storage", zap.Duration("elapsed", time.Since(startdb)), zap.Int("count", len(data)))

	pks := maps.Keys(data)

	shares := c.sharesStorage.List(nil, registrystorage.ByNotLiquidated(), registrystorage.ByOperatorID(c.operatorDataStore.GetOperatorID()), func(share *ssvtypes.SSVShare) bool {
		return slices.Contains(pks, share.ValidatorPubKey)
	})

	for _, share := range shares {
		// Start validator (if not already started).
		// TODO: why its in the map if not started?
		if v, found := c.validatorsMap.GetValidator(share.ValidatorPubKey); found {
			v.UpdateShare(
				func(s *types.SSVShare) {
					s.BeaconMetadata = share.BeaconMetadata
					s.ValidatorIndex = share.ValidatorIndex
				}, func(s *genesistypes.SSVShare) {
					s.BeaconMetadata = share.BeaconMetadata
				},
			)
			_, err := c.startValidator(v)
			if err != nil {
				c.logger.Warn("could not start validator", zap.Error(err))
			}
			vc, found := c.validatorsMap.GetCommittee(v.Share().CommitteeID())
			if found {
				vc.AddShare(&v.Share().Share)
				_, err := c.startCommittee(vc)
				if err != nil {
					c.logger.Warn("could not start committee", zap.Error(err))
				}
			}
		} else {
			c.logger.Info("starting new validator", fields.PubKey(share.ValidatorPubKey[:]))

			started, err := c.onShareStart(share)
			if err != nil {
				c.logger.Warn("could not start newly active validator", zap.Error(err))
				continue
			}
			if started {
				c.logger.Debug("started share after metadata update", zap.Bool("started", started))
			}
		}
	}

	return nil
}

// GetValidator returns a validator instance from ValidatorsMap
func (c *controller) GetValidator(pubKey spectypes.ValidatorPK) (*validators.ValidatorContainer, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

func (c *controller) ExecuteGenesisDuty(logger *zap.Logger, duty *genesisspectypes.Duty) {
	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer,
	// so we need to copy the pubkey to avoid pointer.
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	if v, ok := c.GetValidator(spectypes.ValidatorPK(pk)); ok {
		ssvMsg, err := CreateGenesisDutyExecuteMsg(duty, pk, genesisssvtypes.GetDefaultDomain())
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := genesisqueue.DecodeGenesisSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}
		if pushed := v.GenesisValidator.Queues[duty.Type].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
	} else {
		logger.Warn("could not find validator", fields.PubKey(duty.PubKey[:]))
	}
}

func (c *controller) ExecuteDuty(logger *zap.Logger, duty *spectypes.ValidatorDuty) {
	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	pk := make([]byte, 48)
	copy(pk, duty.PubKey[:])

	if v, ok := c.GetValidator(spectypes.ValidatorPK(pk)); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, c.networkConfig.DomainType())
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}
		if pushed := v.Validator.Queues[duty.RunnerRole()].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	} else {
		logger.Warn("could not find validator")
	}
}

func (c *controller) ExecuteCommitteeDuty(logger *zap.Logger, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty) {
	logger = logger.With(fields.Slot(duty.Slot), fields.Role(duty.RunnerRole()))

	if cm, ok := c.validatorsMap.GetCommittee(committeeID); ok {
		ssvMsg, err := CreateCommitteeDutyExecuteMsg(duty, committeeID, c.networkConfig.DomainType())
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			return
		}
		if err := cm.OnExecuteDuty(logger, dec.Body.(*ssvtypes.EventMsg)); err != nil {
			logger.Error("could not execute committee duty", zap.Error(err))
		}
	} else {
		logger.Warn("could not find committee", fields.CommitteeID(committeeID))
	}
}

// CreateDutyExecuteMsg returns ssvMsg with event type of execute duty
func CreateDutyExecuteMsg(duty *spectypes.ValidatorDuty, pubKey []byte, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := ssvtypes.ExecuteDutyData{Duty: duty}
	data, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, pubKey, duty.RunnerRole(), data)
}

// CreateCommitteeDutyExecuteMsg returns ssvMsg with event type of execute committee duty
func CreateCommitteeDutyExecuteMsg(duty *spectypes.CommitteeDuty, committeeID spectypes.CommitteeID, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeCommitteeDutyData := ssvtypes.ExecuteCommitteeDutyData{Duty: duty}
	data, err := json.Marshal(executeCommitteeDutyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute committee duty data: %w", err)
	}

	return dutyDataToSSVMsg(domain, committeeID[:], spectypes.RoleCommittee, data)
}

func CreateGenesisDutyExecuteMsg(duty *genesisspectypes.Duty, pubKey phase0.BLSPubKey, domain genesisspectypes.DomainType) (*genesisspectypes.SSVMessage, error) {
	executeDutyData := genesisssvtypes.ExecuteDutyData{Duty: duty}
	b, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal execute duty data")
	}
	msg := genesisssvtypes.EventMsg{
		Type: genesisssvtypes.ExecuteDuty,
		Data: b,
	}
	data, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode event msg")
	}
	return &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.MsgType(message.SSVEventMsgType),
		MsgID:   genesisspectypes.NewMsgID(domain, pubKey[:], duty.Type),
		Data:    data,
	}, nil
}

func dutyDataToSSVMsg(
	domain spectypes.DomainType,
	msgIdentifier []byte,
	runnerRole spectypes.RunnerRole,
	data []byte,
) (*spectypes.SSVMessage, error) {
	msg := ssvtypes.EventMsg{
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
		if v.Share().IsAttesting(epoch) {
			indices = append(indices, v.Share().BeaconMetadata.Index)
		}
	}
	return indices
}

func (c *controller) AllActiveIndices(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex {
	if afterInit {
		<-c.committeeValidatorSetup
	}
	var indices []phase0.ValidatorIndex
	c.sharesStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if share.IsAttesting(epoch) {
			indices = append(indices, share.BeaconMetadata.Index)
		}
		return true
	})
	return indices
}

// TODO: this looks like its duplicated behaviour, check if necessary
// onMetadataUpdated is called when validator's metadata was updated
func (c *controller) onMetadataUpdated(pk spectypes.ValidatorPK, meta *beaconprotocol.ValidatorMetadata) {
	if meta == nil {
		return
	}
	logger := c.logger.With(fields.PubKey(pk[:]))

	if v, exist := c.GetValidator(pk); exist {
		// update share object owned by the validator
		// TODO: check if this updates running validators
		if !v.Share().BeaconMetadata.Equals(meta) {
			v.UpdateShare(
				func(s *types.SSVShare) {
					s.BeaconMetadata.Status = meta.Status
					s.BeaconMetadata.Balance = meta.Balance
					s.BeaconMetadata.ActivationEpoch = meta.ActivationEpoch
				},
				func(s *genesistypes.SSVShare) {
					s.BeaconMetadata.Status = meta.Status
					s.BeaconMetadata.Balance = meta.Balance
					s.BeaconMetadata.ActivationEpoch = meta.ActivationEpoch
				},
			)
			logger.Debug("metadata was updated")
		}
		_, err := c.startValidator(v)
		if err != nil {
			logger.Warn("could not start validator after metadata update",
				zap.Error(err), zap.Any("metadata", meta))
		}
		if vc, vcexist := c.validatorsMap.GetCommittee(v.Share().CommitteeID()); vcexist {
			vc.AddShare(&v.Share().Share)
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

	if v == nil {
		c.logger.Warn("could not find validator to stop", fields.PubKey(pubKey[:]))
		return
	}

	// stop instance
	v.Stop()
	c.logger.Debug("validator was stopped", fields.PubKey(pubKey[:]))
	vc, ok := c.validatorsMap.GetCommittee(v.Share().CommitteeID())
	if ok {
		vc.RemoveShare(v.Share().Share.ValidatorIndex)
		if len(vc.Shares) == 0 {
			deletedCommittee := c.validatorsMap.RemoveCommittee(v.Share().CommitteeID())
			if deletedCommittee == nil {
				c.logger.Warn("could not find committee to remove on no validators",
					fields.CommitteeID(v.Share().CommitteeID()),
					fields.PubKey(pubKey[:]),
				)
				return
			}
			deletedCommittee.Stop()
		}
	}
}

func (c *controller) onShareInit(share *ssvtypes.SSVShare) (*validators.ValidatorContainer, *validator.Committee, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		c.logger.Warn("skipping validator until it becomes active", fields.PubKey(share.ValidatorPubKey[:]))
		return nil, nil, nil
	}

	if err := c.setShareFeeRecipient(share, c.recipientsStorage.GetRecipientData); err != nil {
		return nil, nil, fmt.Errorf("could not set share fee recipient: %w", err)
	}

	operator, err := c.committeeMemberFromShare(share)
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
		alanValidator := validator.NewValidator(ctx, cancel, opts)

		genesisOpts := c.genesisValidatorOptions
		// TODO: (Alan) share mutations such as metadata changes and fee recipient updates aren't reflected in genesis shares
		// because shares are duplicated.
		genesisOpts.SSVShare = genesisssvtypes.ConvertToGenesisSSVShare(share, operator)
		genesisOpts.DutyRunners = SetupGenesisRunners(ctx, c.logger, opts)

		genesisValidator := genesisvalidator.NewValidator(ctx, cancel, genesisOpts)

		v = &validators.ValidatorContainer{Validator: alanValidator, GenesisValidator: genesisValidator}
		c.validatorsMap.PutValidator(share.ValidatorPubKey, v)

		c.printShare(share, "setup validator done")

	}

	// Start a committee validator.
	vc, found := c.validatorsMap.GetCommittee(operator.CommitteeID)
	if !found {
		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(c.context)
		_ = cancel

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.Operator = operator

		logger := c.logger.With([]zap.Field{
			zap.String("committee", fields.FormatCommittee(operator.Committee)),
			zap.String("committee_id", hex.EncodeToString(operator.CommitteeID[:])),
		}...)

		committeeRunnerFunc := SetupCommitteeRunners(ctx, opts)

		vc = validator.NewCommittee(c.context, logger, c.beacon.GetBeaconNetwork(), operator, committeeRunnerFunc)
		vc.AddShare(&share.Share)
		c.validatorsMap.PutCommittee(operator.CommitteeID, vc)

		c.printShare(share, "setup committee done")

	} else {
		vc.AddShare(&share.Share)
		c.printShare(share, "added share to committee")
	}

	return v, vc, nil
}

func (c *controller) committeeMemberFromShare(share *ssvtypes.SSVShare) (*spectypes.CommitteeMember, error) {
	operators := make([]*spectypes.Operator, len(share.Committee))
	for i, cm := range share.Committee {
		opdata, found, err := c.operatorsStorage.GetOperatorData(nil, cm.Signer)
		if err != nil {
			return nil, fmt.Errorf("could not get operator data: %w", err)
		}
		if !found {
			//TODO alan: support removed ops
			return nil, fmt.Errorf("operator not found")
		}

		operatorPEM, err := base64.StdEncoding.DecodeString(string(opdata.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("could not decode public key: %w", err)
		}

		operators[i] = &spectypes.Operator{
			OperatorID:        cm.Signer,
			SSVOperatorPubKey: operatorPEM,
		}
	}

	f := ssvtypes.ComputeF(len(share.Committee))

	operatorPEM, err := base64.StdEncoding.DecodeString(string(c.operatorDataStore.GetOperatorData().PublicKey))
	if err != nil {
		return nil, fmt.Errorf("could not decode public key: %w", err)
	}

	return &spectypes.CommitteeMember{
		OperatorID:        c.operatorDataStore.GetOperatorID(),
		CommitteeID:       share.CommitteeID(),
		SSVOperatorPubKey: operatorPEM,
		FaultyNodes:       f,
		Committee:         operators,
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

func (c *controller) validatorStart(validator *validators.ValidatorContainer) (bool, error) {
	if c.validatorStartFunc == nil {
		return validator.Start(c.logger)
	}
	return c.validatorStartFunc(validator)
}

//func (c *controller) startValidatorAndCommittee(v *val)

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *validators.ValidatorContainer) (bool, error) {
	c.reportValidatorStatus(v.Share().ValidatorPubKey[:], v.Share().BeaconMetadata)
	if v.Share().BeaconMetadata.Index == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	started, err := c.validatorStart(v)
	if err != nil {
		c.metrics.ValidatorError(v.Share().ValidatorPubKey[:])
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
	const batchSize = 512
	var sleep = 2 * time.Second

	for {
		// Get the shares to fetch metadata for.
		start := time.Now()
		var existingShares, newShares []*ssvtypes.SSVShare
		c.sharesStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
			if share.Liquidated {
				return true
			}
			if share.BeaconMetadata == nil && share.MetadataLastUpdated().IsZero() {
				newShares = append(newShares, share)
			} else if time.Since(share.MetadataLastUpdated()) > c.metadataUpdateInterval {
				existingShares = append(existingShares, share)
			}
			return len(newShares) < batchSize
		})

		// Combine validators up to batchSize, prioritizing the new ones.
		shares := newShares
		if remainder := batchSize - len(shares); remainder > 0 {
			end := remainder
			if end > len(existingShares) {
				end = len(existingShares)
			}
			shares = append(shares, existingShares[:end]...)
		}
		for _, share := range shares {
			share.SetMetadataLastUpdated(time.Now())
		}

		filteringTook := time.Since(start)
		if len(shares) > 0 {
			pubKeys := make([][]byte, len(shares))
			for i, s := range shares {
				pubKeys[i] = s.ValidatorPubKey[:]
			}
			err := c.updateValidatorsMetadata(c.logger, pubKeys, c, c.beacon, c.onMetadataUpdated)
			if err != nil {
				c.logger.Warn("failed to update validators metadata", zap.Error(err))
				continue
			}
		}
		c.logger.Debug("updated validators metadata",
			zap.Int("validators", len(shares)),
			zap.Int("new_validators", len(newShares)),
			zap.Uint64("started_validators", c.recentlyStartedValidators),
			zap.Duration("filtering_took", filteringTook),
			fields.Took(time.Since(start)))

		// Only sleep if there aren't more validators to fetch metadata for.
		if len(shares) < batchSize {
			time.Sleep(sleep)
		}
	}
}

func (c *controller) updateValidatorsMetadata(logger *zap.Logger, pks [][]byte, storage beaconprotocol.ValidatorMetadataStorage, beacon beaconprotocol.BeaconNode, onMetadataUpdated func(pk spectypes.ValidatorPK, meta *beaconprotocol.ValidatorMetadata)) error {
	// Fetch metadata for all validators.
	c.recentlyStartedValidators = 0
	beforeUpdate := c.AllActiveIndices(c.beacon.GetBeaconNetwork().EstimatedCurrentEpoch(), false)

	err := beaconprotocol.UpdateValidatorsMetadata(logger, pks, storage, beacon, onMetadataUpdated)
	if err != nil {
		return errors.Wrap(err, "failed to update validators metadata")
	}

	// Refresh duties if there are any new active validators.
	afterUpdate := c.AllActiveIndices(c.beacon.GetBeaconNetwork().EstimatedCurrentEpoch(), false)
	if c.recentlyStartedValidators > 0 || hasNewValidators(beforeUpdate, afterUpdate) {
		c.logger.Debug("new validators found after metadata update",
			zap.Int("before", len(beforeUpdate)),
			zap.Int("after", len(afterUpdate)),
			zap.Uint64("started_validators", c.recentlyStartedValidators),
		)
		select {
		case c.indicesChange <- struct{}{}:
		case <-time.After(2 * c.beacon.GetBeaconNetwork().SlotDurationSec()):
			c.logger.Warn("timed out while notifying DutyScheduler of new validators")
		}
	}
	return nil
}

func hasNewValidators(before []phase0.ValidatorIndex, after []phase0.ValidatorIndex) bool {
	m := make(map[phase0.ValidatorIndex]struct{})
	for _, v := range before {
		m[v] = struct{}{}
	}
	for _, v := range after {
		if _, ok := m[v]; !ok {
			return true
		}
	}
	return false
}

func SetupCommitteeRunners(
	ctx context.Context,
	options validator.Options,
) validator.CommitteeRunnerFunc {
	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner: options.Signer,
			Domain:       options.NetworkConfig.DomainType(),
			ValueCheckF:  nil, // sets per role type
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := specqbft.RoundRobinProposer(state, round)
				//logger.Debug("leader", zap.Int("operator_id", int(leader)))
				return leader
			},
			Storage:     options.Storage.Get(convert.RunnerRole(role)),
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig.Beacon, role, nil),
			CutOffRound: specqbft.Round(specqbft.CutoffRound),
		}
		config.ValueCheckF = valueCheckF

		identifier := spectypes.NewMsgID(options.NetworkConfig.DomainType(), options.Operator.CommitteeID[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.OperatorSigner, options.FullNode)
		return qbftCtrl
	}

	return func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share, slashableValidators []spectypes.ShareValidatorPK) *runner.CommitteeRunner {
		// Create a committee runner.
		epoch := options.NetworkConfig.Beacon.GetBeaconNetwork().EstimatedEpochAtSlot(slot)
		valCheck := specssv.BeaconVoteValueCheckF(options.Signer, slot, slashableValidators, epoch)
		crunner := runner.NewCommitteeRunner(
			options.NetworkConfig,
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
			BeaconSigner: options.Signer,
			Domain:       options.NetworkConfig.DomainType(),
			ValueCheckF:  nil, // sets per role type
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := specqbft.RoundRobinProposer(state, round)
				//logger.Debug("leader", zap.Int("operator_id", int(leader)))
				return leader
			},
			Storage:     options.Storage.Get(convert.RunnerRole(role)),
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig.Beacon, role, nil),
			CutOffRound: specqbft.Round(specqbft.CutoffRound),
		}
		config.ValueCheckF = valueCheckF

		alanDomainType := options.NetworkConfig.AlanDomainType

		identifier := spectypes.NewMsgID(alanDomainType, options.SSVShare.Share.ValidatorPubKey[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.OperatorSigner, options.FullNode)
		return qbftCtrl
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share) // TODO: fill the map
	shareMap[options.SSVShare.ValidatorIndex] = &options.SSVShare.Share

	runners := runner.ValidatorDutyRunners{}
	alanDomainType := options.NetworkConfig.AlanDomainType
	for _, role := range runnersType {
		switch role {
		//case spectypes.BNRoleAttester:
		//	valCheck := specssv.AttesterValueCheckF(options.Signer, options.NetworkConfig.Beacon.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
		//	qbftCtrl := buildController(spectypes.BNRoleAttester, valCheck)
		//	runners[role] = runner.NewAttesterRunner(options.NetworkConfig.Beacon.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, valCheck, 0)
		case spectypes.RoleProposer:
			proposedValueCheck := specssv.ProposerValueCheckF(options.Signer, options.NetworkConfig.Beacon.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(spectypes.RoleProposer, proposedValueCheck)
			runners[role] = runner.NewProposerRunner(alanDomainType, options.NetworkConfig.Beacon.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, proposedValueCheck, 0, options.Graffiti)
		case spectypes.RoleAggregator:
			aggregatorValueCheckF := specssv.AggregatorValueCheckF(options.Signer, options.NetworkConfig.Beacon.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.RoleAggregator, aggregatorValueCheckF)
			runners[role] = runner.NewAggregatorRunner(alanDomainType, options.NetworkConfig.Beacon.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, aggregatorValueCheckF, 0)
		//case spectypes.BNRoleSyncCommittee:
		//syncCommitteeValueCheckF := specssv.SyncCommitteeValueCheckF(options.Signer, options.NetworkConfig.Beacon.GetBeaconNetwork(), options.SSVShare.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
		//qbftCtrl := buildController(spectypes.BNRoleSyncCommittee, syncCommitteeValueCheckF)
		//runners[role] = runner.NewSyncCommitteeRunner(options.NetworkConfig, options.NetworkConfig.Beacon.GetBeaconNetwork(), &options.SSVShare.Share, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeValueCheckF, 0)
		case spectypes.RoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := specssv.SyncCommitteeContributionValueCheckF(options.Signer, options.NetworkConfig.Beacon.GetBeaconNetwork(), options.SSVShare.Share.ValidatorPubKey, options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(spectypes.RoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role] = runner.NewSyncCommitteeAggregatorRunner(alanDomainType, options.NetworkConfig.Beacon.GetBeaconNetwork(), shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeContributionValueCheckF, 0)
		case spectypes.RoleValidatorRegistration:
			runners[role] = runner.NewValidatorRegistrationRunner(alanDomainType, options.NetworkConfig.Beacon.GetBeaconNetwork(), shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		case spectypes.RoleVoluntaryExit:
			runners[role] = runner.NewVoluntaryExitRunner(alanDomainType, options.NetworkConfig.Beacon.GetBeaconNetwork(), shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		}
	}
	return runners
}

func SetupGenesisRunners(ctx context.Context, logger *zap.Logger, options validator.Options) genesisrunner.DutyRunners {
	if options.SSVShare == nil || options.SSVShare.BeaconMetadata == nil {
		logger.Error("missing validator metadata", zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey[:])))
		return genesisrunner.DutyRunners{} // TODO need to find better way to fix it
	}

	runnersType := []genesisspectypes.BeaconRole{
		genesisspectypes.BNRoleAttester,
		genesisspectypes.BNRoleProposer,
		genesisspectypes.BNRoleAggregator,
		genesisspectypes.BNRoleSyncCommittee,
		genesisspectypes.BNRoleSyncCommitteeContribution,
		genesisspectypes.BNRoleValidatorRegistration,
		genesisspectypes.BNRoleVoluntaryExit,
	}

	share := genesisssvtypes.ConvertToGenesisShare(&options.SSVShare.Share, options.Operator)

	buildController := func(role genesisspectypes.BeaconRole, valueCheckF genesisspecqbft.ProposedValueCheckF) *genesisqbftcontroller.Controller {
		config := &genesisqbft.Config{
			Signer:      options.GenesisOptions.Signer,
			SigningPK:   options.SSVShare.ValidatorPubKey[:],
			Domain:      genesisssvtypes.GetDefaultDomain(),
			ValueCheckF: nil, // sets per role type
			ProposerF: func(state *genesisspecqbft.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
				leader := genesisspecqbft.RoundRobinProposer(state, round)
				return leader
			},
			Storage: options.GenesisOptions.Storage.Get(role),
			Network: options.GenesisOptions.Network,
			Timer:   genesisroundtimer.New(ctx, options.NetworkConfig.Beacon, role, nil),
		}
		config.ValueCheckF = valueCheckF
		identifier := genesisspectypes.NewMsgID(genesisssvtypes.GetDefaultDomain(), options.SSVShare.Share.ValidatorPubKey[:], role)
		qbftCtrl := genesisqbftcontroller.NewController(identifier[:], share, config, options.FullNode)
		qbftCtrl.NewDecidedHandler = options.GenesisOptions.NewDecidedHandler
		return qbftCtrl
	}

	genesisBeaconNetwork := genesisspectypes.BeaconNetwork(options.NetworkConfig.Beacon.GetBeaconNetwork())

	runners := genesisrunner.DutyRunners{}
	genesisDomainType := options.NetworkConfig.GenesisDomainType
	for _, role := range runnersType {
		switch role {
		case genesisspectypes.BNRoleAttester:
			valCheck := genesisspecssv.AttesterValueCheckF(options.GenesisOptions.Signer, genesisBeaconNetwork, options.SSVShare.Share.ValidatorPubKey[:], options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(genesisspectypes.BNRoleAttester, valCheck)
			runners[role] = genesisrunner.NewAttesterRunnner(genesisDomainType, genesisBeaconNetwork, share, qbftCtrl, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer, valCheck, 0)
		case genesisspectypes.BNRoleProposer:
			proposedValueCheck := genesisspecssv.ProposerValueCheckF(options.GenesisOptions.Signer, genesisBeaconNetwork, options.SSVShare.Share.ValidatorPubKey[:], options.SSVShare.BeaconMetadata.Index, options.SSVShare.SharePubKey)
			qbftCtrl := buildController(genesisspectypes.BNRoleProposer, proposedValueCheck)
			runners[role] = genesisrunner.NewProposerRunner(genesisDomainType, genesisBeaconNetwork, share, qbftCtrl, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer, proposedValueCheck, 0, options.Graffiti)
		case genesisspectypes.BNRoleAggregator:
			aggregatorValueCheckF := genesisspecssv.AggregatorValueCheckF(options.GenesisOptions.Signer, genesisBeaconNetwork, options.SSVShare.Share.ValidatorPubKey[:], options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(genesisspectypes.BNRoleAggregator, aggregatorValueCheckF)
			runners[role] = genesisrunner.NewAggregatorRunner(genesisDomainType, genesisBeaconNetwork, share, qbftCtrl, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer, aggregatorValueCheckF, 0)
		case genesisspectypes.BNRoleSyncCommittee:
			syncCommitteeValueCheckF := genesisspecssv.SyncCommitteeValueCheckF(options.GenesisOptions.Signer, genesisBeaconNetwork, options.SSVShare.ValidatorPubKey[:], options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(genesisspectypes.BNRoleSyncCommittee, syncCommitteeValueCheckF)
			runners[role] = genesisrunner.NewSyncCommitteeRunner(genesisDomainType, genesisBeaconNetwork, share, qbftCtrl, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer, syncCommitteeValueCheckF, 0)
		case genesisspectypes.BNRoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := genesisspecssv.SyncCommitteeContributionValueCheckF(options.GenesisOptions.Signer, genesisBeaconNetwork, options.SSVShare.Share.ValidatorPubKey[:], options.SSVShare.BeaconMetadata.Index)
			qbftCtrl := buildController(genesisspectypes.BNRoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role] = genesisrunner.NewSyncCommitteeAggregatorRunner(genesisDomainType, genesisBeaconNetwork, share, qbftCtrl, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer, syncCommitteeContributionValueCheckF, 0)
		case genesisspectypes.BNRoleValidatorRegistration:
			runners[role] = genesisrunner.NewValidatorRegistrationRunner(genesisDomainType, genesisBeaconNetwork, share, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer)
		case genesisspectypes.BNRoleVoluntaryExit:
			runners[role] = genesisrunner.NewVoluntaryExitRunner(genesisDomainType, genesisBeaconNetwork, share, options.GenesisBeacon, options.GenesisOptions.Network, options.GenesisOptions.Signer)
		}
	}
	return runners
}
