package validator

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator/metadata"
	"github.com/ssvlabs/ssv/operator/validators"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/queue/worker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

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
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating validator metadata"` // used outside of validator controller, left for compatibility
	HistorySyncBatchSize       int           `yaml:"HistorySyncBatchSize" env:"HISTORY_SYNC_BATCH_SIZE" env-default:"25" env-description:"Maximum number of messages to sync in a single batch"`
	MinPeers                   int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"Minimum number of peers required for sync"`
	Network                    P2PNetwork
	Beacon                     beaconprotocol.BeaconNode
	FullNode                   bool   `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Store complete message history instead of just latest messages"`
	Exporter                   bool   `yaml:"Exporter" env:"EXPORTER" env-default:"false" env-description:"Enable data export functionality"`
	ExporterRetainSlots        uint64 `yaml:"ExporterRetainSlots" env:"EXPORTER_RETAIN_SLOTS" env-default:"50400" env-description:"Number of slots to retain in export data"`
	BeaconSigner               ekm.BeaconSigner
	OperatorSigner             ssvtypes.OperatorSigner
	OperatorDataStore          operatordatastore.OperatorDataStore
	RegistryStorage            nodestorage.Storage
	RecipientsStorage          Recipients
	NewDecidedHandler          qbftcontroller.NewDecidedHandler
	DutyRoles                  []spectypes.BeaconRole
	StorageMap                 *storage.ParticipantStores
	ValidatorStore             registrystorage.ValidatorStore
	MessageValidator           validation.MessageValidator
	ValidatorsMap              *validators.ValidatorsMap
	DoppelgangerHandler        doppelganger.Provider
	NetworkConfig              networkconfig.Network
	ValidatorSyncer            *metadata.Syncer
	Graffiti                   []byte
	ProposerDelay              time.Duration
	KeyManager                 ekm.KeyManager

	// worker flags
	WorkersCount    int    `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"256" env-description:"Number of message processing workers"`
	QueueBufferSize int    `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"65536" env-description:"Size of message worker queue buffer"`
	GasLimit        uint64 `yaml:"ExperimentalGasLimit" env:"EXPERIMENTAL_GAS_LIMIT" env-default:"30000000" env-description:"Gas limit for MEV block proposals (must match across committee, otherwise MEV fails). Do not change unless you know what you're doing"`
}

// Controller represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type Controller interface {
	StartValidators(ctx context.Context)
	HandleMetadataUpdates(ctx context.Context)
	FilterIndices(afterInit bool, filter func(*ssvtypes.SSVShare) bool) []phase0.ValidatorIndex
	GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool)
	StartNetworkHandlers()
	// GetValidatorStats returns stats of validators, including the following:
	//  - the amount of validators in the network
	//  - the amount of active validators (i.e. not slashed or existed)
	//  - the amount of validators assigned to this operator
	GetValidatorStats() (uint64, uint64, uint64, error)
	IndicesChangeChan() chan struct{}
	ValidatorExitChan() <-chan registrystorage.ExitDescriptor

	// TODO: remove it and use callbacks?
	StopValidator(pubKey spectypes.ValidatorPK) error
	ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex, ownValidator bool) error
	ReportValidatorStatuses(ctx context.Context)
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
	Get(txn basedb.Reader, pubKey []byte) (*ssvtypes.SSVShare, bool)
	List(txn basedb.Reader, filters ...registrystorage.SharesFilter) []*ssvtypes.SSVShare
	Range(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool)
}

type P2PNetwork interface {
	protocolp2p.Broadcaster
	UseMessageRouter(router network.MessageRouter)
	SubscribeRandoms(numSubnets int) error
	ActiveSubnets() commons.Subnets
	FixedSubnets() commons.Subnets
}

// controller implements Controller
type controller struct {
	ctx context.Context

	logger *zap.Logger

	networkConfig     networkconfig.Network
	sharesStorage     SharesStorage
	operatorsStorage  registrystorage.Operators
	recipientsStorage Recipients
	ibftStorageMap    *storage.ParticipantStores

	beacon         beaconprotocol.BeaconNode
	beaconSigner   ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	keyManager     ekm.KeyManager

	operatorDataStore operatordatastore.OperatorDataStore

	validatorOptions        validator.Options
	validatorStore          registrystorage.ValidatorStore
	validatorsMap           *validators.ValidatorsMap
	validatorStartFunc      func(validator *validator.Validator) (bool, error)
	committeeValidatorSetup chan struct{}
	dutyGuard               *validator.CommitteeDutyGuard

	validatorSyncer *metadata.Syncer

	operatorsIDs         *sync.Map
	network              P2PNetwork
	messageRouter        *messageRouter
	messageWorker        *worker.Worker
	historySyncBatchSize int
	messageValidator     validation.MessageValidator

	// nonCommittees is a cache of initialized committeeObserver instances
	committeesObservers      *ttlcache.Cache[spectypes.MessageID, *committeeObserver]
	committeesObserversMutex sync.Mutex

	attesterRoots   *ttlcache.Cache[phase0.Root, struct{}]
	syncCommRoots   *ttlcache.Cache[phase0.Root, struct{}]
	beaconVoteRoots *ttlcache.Cache[validator.BeaconVoteCacheKey, struct{}]

	domainCache *validator.DomainCache

	indicesChangeCh chan struct{}
	validatorExitCh chan registrystorage.ExitDescriptor
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
		Storage:       options.StorageMap,
		//Share:   nil,  // set per validator
		Signer:              options.BeaconSigner,
		OperatorSigner:      options.OperatorSigner,
		DoppelgangerHandler: options.DoppelgangerHandler,
		DutyRunners:         nil, // set per validator
		NewDecidedHandler:   options.NewDecidedHandler,
		FullNode:            options.FullNode,
		Exporter:            options.Exporter,
		GasLimit:            options.GasLimit,
		MessageValidator:    options.MessageValidator,
		Graffiti:            options.Graffiti,
		ProposerDelay:       options.ProposerDelay,
	}

	// If full node, increase queue size to make enough room
	// for history sync batches to be pushed whole.
	if options.FullNode {
		size := options.HistorySyncBatchSize * 2
		if size > validator.DefaultQueueSize {
			validatorOptions.QueueSize = size
		}
	}

	cacheTTL := 2 * options.NetworkConfig.EpochDuration() // #nosec G115

	ctrl := controller{
		logger:            logger.Named(logging.NameController),
		networkConfig:     options.NetworkConfig,
		sharesStorage:     options.RegistryStorage.Shares(),
		operatorsStorage:  options.RegistryStorage,
		recipientsStorage: options.RegistryStorage,
		ibftStorageMap:    options.StorageMap,
		validatorStore:    options.ValidatorStore,
		ctx:               options.Context,
		beacon:            options.Beacon,
		operatorDataStore: options.OperatorDataStore,
		beaconSigner:      options.BeaconSigner,
		operatorSigner:    options.OperatorSigner,
		keyManager:        options.KeyManager,
		network:           options.Network,

		validatorsMap:    options.ValidatorsMap,
		validatorOptions: validatorOptions,

		validatorSyncer: options.ValidatorSyncer,

		operatorsIDs: operatorsIDs,

		messageRouter:        newMessageRouter(logger),
		messageWorker:        worker.NewWorker(logger, workerCfg),
		historySyncBatchSize: options.HistorySyncBatchSize,

		committeesObservers: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *committeeObserver](cacheTTL),
		),
		attesterRoots: ttlcache.New(
			ttlcache.WithTTL[phase0.Root, struct{}](cacheTTL),
		),
		syncCommRoots: ttlcache.New(
			ttlcache.WithTTL[phase0.Root, struct{}](cacheTTL),
		),
		domainCache: validator.NewDomainCache(options.Beacon, cacheTTL),
		beaconVoteRoots: ttlcache.New(
			ttlcache.WithTTL[validator.BeaconVoteCacheKey, struct{}](cacheTTL),
		),
		indicesChangeCh:         make(chan struct{}),
		validatorExitCh:         make(chan registrystorage.ExitDescriptor),
		committeeValidatorSetup: make(chan struct{}, 1),
		dutyGuard:               validator.NewCommitteeDutyGuard(),

		messageValidator: options.MessageValidator,
	}

	// Register lifecycle callbacks with ValidatorStore
	ctrl.registerValidatorStoreCallbacks()

	// Start automatic expired item deletion in nonCommitteeValidators.
	go ctrl.committeesObservers.Start()
	// Delete old root and domain entries.
	go ctrl.attesterRoots.Start()
	go ctrl.syncCommRoots.Start()
	go ctrl.domainCache.Start()
	go ctrl.beaconVoteRoots.Start()

	return &ctrl
}

// registerValidatorStoreCallbacks sets up the lifecycle callbacks for ValidatorStore
func (c *controller) registerValidatorStoreCallbacks() {
	callbacks := registrystorage.ValidatorLifecycleCallbacks{
		OnValidatorAdded: func(ctx context.Context, snapshot *registrystorage.ValidatorSnapshot) error {
			// For new validators, we don't need to do anything special here
			// as they will be initialized and started through OnValidatorStarted
			c.logger.Debug("validator added to store",
				fields.PubKey(snapshot.Share.ValidatorPubKey[:]),
				zap.Bool("has_metadata", snapshot.Share.HasBeaconMetadata()))
			return nil
		},

		OnValidatorStarted: func(ctx context.Context, snapshot *registrystorage.ValidatorSnapshot) error {
			// This is called when a validator should start participating
			c.logger.Debug("validator should start", fields.PubKey(snapshot.Share.ValidatorPubKey[:]))

			// Make a copy of the share for thread safety
			share := snapshot.Share.Copy()

			// Start the validator asynchronously to avoid blocking
			go func() {
				started, err := c.onShareStart(share)
				if err != nil {
					c.logger.Error("failed to start validator from lifecycle callback",
						fields.PubKey(share.ValidatorPubKey[:]),
						zap.Error(err))
					return
				}
				if started {
					c.logger.Debug("validator started from lifecycle callback",
						fields.PubKey(share.ValidatorPubKey[:]))

					// Notify indices change
					if !c.reportIndicesChange(ctx) {
						c.logger.Error("failed to notify indices change")
					}
				}
			}()

			return nil
		},

		OnValidatorStopped: func(ctx context.Context, pubKey spectypes.ValidatorPK) error {
			c.logger.Debug("validator should stop", fields.PubKey(pubKey[:]))

			// Stop the validator asynchronously to avoid blocking
			go func() {
				c.onShareStop(pubKey)
			}()

			return nil
		},

		OnValidatorUpdated: func(ctx context.Context, snapshot *registrystorage.ValidatorSnapshot) error {
			c.logger.Debug("validator updated", fields.PubKey(snapshot.Share.ValidatorPubKey[:]))

			// Update fee recipient if it changed
			if v, found := c.validatorsMap.GetValidator(snapshot.Share.ValidatorPubKey); found {
				v.Share.SetFeeRecipient(snapshot.Share.FeeRecipientAddress)
			}

			return nil
		},

		OnValidatorRemoved: func(ctx context.Context, pubKey spectypes.ValidatorPK) error {
			c.logger.Debug("validator removed", fields.PubKey(pubKey[:]))

			// Stop the validator asynchronously
			go func() {
				c.onShareStop(pubKey)
			}()

			return nil
		},

		OnValidatorExited: func(ctx context.Context, descriptor registrystorage.ExitDescriptor) error {
			c.logger.Debug("validator exited",
				fields.PubKey(descriptor.PubKey[:]),
				zap.Uint64("block_number", descriptor.BlockNumber))

			// Send to exit channel asynchronously
			go func() {
				select {
				case c.validatorExitCh <- descriptor:
					c.logger.Debug("sent validator exit descriptor")
				case <-time.After(2 * c.networkConfig.GetSlotDuration()):
					c.logger.Error("timeout sending validator exit descriptor")
				}
			}()

			return nil
		},

		OnIndicesChanged: func(ctx context.Context) error {
			if !c.reportIndicesChange(ctx) {
				return fmt.Errorf("failed to report indices change")
			}
			return nil
		},
	}

	c.validatorStore.RegisterLifecycleCallbacks(callbacks)
}

func (c *controller) IndicesChangeChan() chan struct{} {
	return c.indicesChangeCh
}

func (c *controller) ValidatorExitChan() <-chan registrystorage.ExitDescriptor {
	return c.validatorExitCh
}

func (c *controller) GetValidatorStats() (uint64, uint64, uint64, error) {
	// Use ValidatorStore for statistics
	allValidators := c.validatorStore.GetAllValidators()
	operatorValidators := c.validatorStore.GetSelfValidators()

	active := uint64(0)
	epoch := c.networkConfig.EstimatedCurrentEpoch()

	for _, snapshot := range allValidators {
		if snapshot.Share.IsParticipating(c.networkConfig, epoch) {
			active++
		}
	}

	return uint64(len(allValidators)), active, uint64(len(operatorValidators)), nil
}

func (c *controller) handleRouterMessages() {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	ch := c.messageRouter.GetMessageChan()
	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("router message handler stopped")
			return

		case msg := <-ch:
			switch m := msg.(type) {
			case *queue.SSVMessage:
				if m.MsgType == message.SSVEventMsgType {
					continue
				}

				// TODO: only try copying clusterid if validator failed
				dutyExecutorID := m.GetID().GetDutyExecutorID()
				var cid spectypes.CommitteeID
				copy(cid[:], dutyExecutorID[16:])

				if v, ok := c.validatorsMap.GetValidator(spectypes.ValidatorPK(dutyExecutorID)); ok {
					v.HandleMessage(ctx, c.logger, m)
				} else if vc, ok := c.validatorsMap.GetCommittee(cid); ok {
					vc.HandleMessage(ctx, c.logger, m)
				} else if c.validatorOptions.Exporter {
					if m.MsgType != spectypes.SSVConsensusMsgType && m.MsgType != spectypes.SSVPartialSignatureMsgType {
						continue
					}
					if !c.messageWorker.TryEnqueue(m) {
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

var nonCommitteeValidatorTTLs = map[spectypes.RunnerRole]int{
	spectypes.RoleCommittee:                 64,
	spectypes.RoleProposer:                  4,
	spectypes.RoleAggregator:                4,
	spectypes.RoleSyncCommitteeContribution: 4,
}

func (c *controller) handleWorkerMessages(ctx context.Context, msg network.DecodedSSVMessage) error {
	var ncv *committeeObserver
	ssvMsg := msg.(*queue.SSVMessage)

	item := c.getNonCommitteeValidators(ssvMsg.GetID())
	if item == nil {
		committeeObserverOptions := validator.CommitteeObserverOptions{
			Logger:            c.logger,
			BeaconConfig:      c.networkConfig,
			ValidatorStore:    c.validatorStore,
			Network:           c.validatorOptions.Network,
			Storage:           c.validatorOptions.Storage,
			FullNode:          c.validatorOptions.FullNode,
			Operator:          c.validatorOptions.Operator,
			OperatorSigner:    c.validatorOptions.OperatorSigner,
			NewDecidedHandler: c.validatorOptions.NewDecidedHandler,
			AttesterRoots:     c.attesterRoots,
			SyncCommRoots:     c.syncCommRoots,
			DomainCache:       c.domainCache,
			BeaconVoteRoots:   c.beaconVoteRoots,
		}
		ncv = &committeeObserver{
			CommitteeObserver: validator.NewCommitteeObserver(ssvMsg.GetID(), committeeObserverOptions),
		}
		ttlSlots := nonCommitteeValidatorTTLs[ssvMsg.MsgID.GetRoleType()]
		c.committeesObservers.Set(
			ssvMsg.GetID(),
			ncv,
			time.Duration(ttlSlots)*c.networkConfig.GetSlotDuration(),
		)
	} else {
		ncv = item
	}
	if err := c.handleNonCommitteeMessages(ctx, ssvMsg, ncv); err != nil {
		return err
	}
	return nil
}

func (c *controller) handleNonCommitteeMessages(
	ctx context.Context,
	msg *queue.SSVMessage,
	ncv *committeeObserver,
) error {
	c.committeesObserversMutex.Lock()
	defer c.committeesObserversMutex.Unlock()

	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		// Process proposal messages for committee consensus only to get the roots
		if msg.MsgID.GetRoleType() != spectypes.RoleCommittee {
			return nil
		}

		subMsg, ok := msg.Body.(*specqbft.Message)
		if !ok || subMsg.MsgType != specqbft.ProposalMsgType {
			return nil
		}

		return ncv.OnProposalMsg(ctx, msg)
	case spectypes.SSVPartialSignatureMsgType:
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
func (c *controller) StartValidators(ctx context.Context) {
	// Get own validators from ValidatorStore
	ownValidators := c.validatorStore.GetSelfValidators()

	if len(ownValidators) == 0 {
		close(c.committeeValidatorSetup)
		c.logger.Info("could not find validators")

		// Subscribe to random subnet if no validators
		if !c.validatorOptions.Exporter {
			if err := c.network.SubscribeRandoms(1); err != nil {
				c.logger.Error("failed to subscribe to random subnets", zap.Error(err))
			}
		}
		return
	}

	// Convert snapshots to shares
	var ownShares []*ssvtypes.SSVShare
	for _, snapshot := range ownValidators {
		share := snapshot.Share.Copy()
		ownShares = append(ownShares, share)
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
			if err := c.network.SubscribeRandoms(1); err != nil {
				c.logger.Error("failed to subscribe to random subnets", zap.Error(err))
			}
		}
		close(c.committeeValidatorSetup)

		// Start validators.
		c.startValidators(inited, committees)
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

	started += len(committees)

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

// GetValidator returns a validator instance from ValidatorsMap
func (c *controller) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

func (c *controller) ExecuteDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty) {
	dutyID := fields.FormatDutyID(c.networkConfig.EstimatedEpochAtSlot(duty.Slot), duty.Slot, duty.Type, duty.ValidatorIndex)
	ctx, span := tracer.Start(observability.TraceContext(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "execute_duty"),
		trace.WithAttributes(
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.BeaconSlotAttribute(duty.Slot),
			observability.BeaconRoleAttribute(duty.Type),
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.ValidatorPublicKeyAttribute(duty.PubKey),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	pk := make([]byte, 48)
	copy(pk, duty.PubKey[:])

	if v, ok := c.GetValidator(spectypes.ValidatorPK(pk)); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, c.networkConfig.GetDomainType())
		if err != nil {
			logger.Error("could not create duty execute msg", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			logger.Error("could not decode duty execute msg", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		dec.TraceContext = ctx
		span.AddEvent("pushing message to the queue")
		if pushed := v.Queues[duty.RunnerRole()].Q.TryPush(dec); !pushed {
			const eventMsg = "dropping ExecuteDuty message because the queue is full"
			logger.Warn(eventMsg)
			span.AddEvent(eventMsg)
		}
	} else {
		const eventMsg = "could not find validator"
		logger.Warn(eventMsg)
		span.AddEvent(eventMsg)
	}

	span.SetStatus(codes.Ok, "")
}

func (c *controller) ExecuteCommitteeDuty(ctx context.Context, logger *zap.Logger, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty) {
	cm, ok := c.validatorsMap.GetCommittee(committeeID)
	if !ok {
		const eventMsg = "could not find committee"
		logger.Warn(eventMsg, fields.CommitteeID(committeeID))
		return
	}

	var committee []spectypes.OperatorID
	for _, operator := range cm.CommitteeMember.Committee {
		committee = append(committee, operator.OperatorID)
	}

	dutyID := fields.FormatCommitteeDutyID(committee, c.networkConfig.EstimatedEpochAtSlot(duty.Slot), duty.Slot)
	ctx, span := tracer.Start(observability.TraceContext(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "execute_committee_duty"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(duty.Slot),
			observability.CommitteeIDAttribute(committeeID),
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	ssvMsg, err := CreateCommitteeDutyExecuteMsg(duty, committeeID, c.networkConfig.GetDomainType())
	if err != nil {
		logger.Error("could not create duty execute msg", zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}
	dec, err := queue.DecodeSSVMessage(ssvMsg)
	if err != nil {
		logger.Error("could not decode duty execute msg", zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}
	if err := cm.OnExecuteDuty(ctx, logger, dec.Body.(*ssvtypes.EventMsg)); err != nil {
		logger.Error("could not execute committee duty", zap.Error(err))
		span.RecordError(err)
	}

	span.SetStatus(codes.Ok, "")
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

func (c *controller) FilterIndices(afterInit bool, filter func(*ssvtypes.SSVShare) bool) []phase0.ValidatorIndex {
	if afterInit {
		<-c.committeeValidatorSetup
	}

	allValidators := c.validatorStore.GetAllValidators()
	var indices []phase0.ValidatorIndex

	for _, snapshot := range allValidators {
		if filter(&snapshot.Share) {
			indices = append(indices, snapshot.Share.ValidatorIndex)
		}
	}

	return indices
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
	vc, ok := c.validatorsMap.GetCommittee(v.Share.CommitteeID())
	if ok {
		vc.RemoveShare(v.Share.ValidatorIndex)
		if len(vc.Shares) == 0 {
			deletedCommittee := c.validatorsMap.RemoveCommittee(v.Share.CommitteeID())
			if deletedCommittee == nil {
				c.logger.Warn("could not find committee to remove on no validators",
					fields.CommitteeID(v.Share.CommitteeID()),
					fields.PubKey(pubKey[:]),
				)
				return
			}
			deletedCommittee.Stop()
		}
	}
}

func (c *controller) onShareInit(share *ssvtypes.SSVShare) (*validator.Validator, *validator.Committee, error) {
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
		validatorCtx, validatorCancel := context.WithCancel(c.ctx)

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.Operator = operator
		opts.DutyRunners, err = SetupRunners(validatorCtx, c.logger, opts)
		if err != nil {
			validatorCancel()
			return nil, nil, fmt.Errorf("could not setup runners: %w", err)
		}

		v = validator.NewValidator(validatorCtx, validatorCancel, opts)
		c.validatorsMap.PutValidator(share.ValidatorPubKey, v)

		c.printShare(share, "setup validator done")
	} else {
		c.printShare(v.Share, "get validator")
	}

	// Start a committee validator.
	vc, found := c.validatorsMap.GetCommittee(operator.CommitteeID)
	if !found {
		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(c.ctx)

		opts := c.validatorOptions
		opts.SSVShare = share
		opts.Operator = operator

		committeeOpIDs := ssvtypes.OperatorIDsFromOperators(operator.Committee)

		logger := c.logger.With([]zap.Field{
			zap.String("committee", fields.FormatCommittee(committeeOpIDs)),
			zap.String("committee_id", hex.EncodeToString(operator.CommitteeID[:])),
		}...)

		committeeRunnerFunc := SetupCommitteeRunners(ctx, opts)

		vc = validator.NewCommittee(
			ctx,
			cancel,
			logger,
			c.networkConfig,
			operator,
			committeeRunnerFunc,
			nil,
			c.dutyGuard,
		)
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

		operatorPEM, err := base64.StdEncoding.DecodeString(opdata.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("could not decode public key: %w", err)
		}

		operators[i] = &spectypes.Operator{
			OperatorID:        cm.Signer,
			SSVOperatorPubKey: operatorPEM,
		}
	}

	f := ssvtypes.ComputeF(uint64(len(share.Committee)))

	operatorPEM, err := base64.StdEncoding.DecodeString(c.operatorDataStore.GetOperatorData().PublicKey)
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
	v, _, err := c.onShareInit(share)
	if err != nil || v == nil {
		return false, err
	}

	started, err := c.startValidator(v)
	if err != nil {
		return false, err
	}

	return started, nil
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

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *validator.Validator) (bool, error) {
	c.reportValidatorStatus(v.Share)
	if v.Share.ValidatorIndex == 0 {
		return false, errors.New("could not start validator: index not found")
	}
	started, err := c.validatorStart(v)
	if err != nil {
		validatorErrorsCounter.Add(c.ctx, 1)
		return false, errors.Wrap(err, "could not start validator")
	}

	return started, nil
}

func (c *controller) HandleMetadataUpdates(ctx context.Context) {
	// TODO: Consider getting rid of `Stream` method because it adds complexity.
	// Instead, validatorSyncer could return the next batch, which would be passed to handleMetadataUpdate afterwards.
	// There doesn't seem to exist any logic that requires these processes to be parallel.
	for syncBatch := range c.validatorSyncer.Stream(ctx) {
		if err := c.handleMetadataUpdate(ctx, syncBatch); err != nil {
			c.logger.Warn("could not handle metadata sync", zap.Error(err))
		}
	}
}

// handleMetadataUpdate processes metadata changes for validators.
func (c *controller) handleMetadataUpdate(ctx context.Context, syncBatch metadata.SyncBatch) error {
	// Skip processing for full nodes (exporters) and operators that are still syncing
	// (i.e., haven't received their OperatorAdded event yet).
	if !c.operatorDataStore.OperatorIDReady() {
		return nil
	}

	// Update ValidatorStore with the new metadata
	changedMetadata, err := c.validatorStore.UpdateValidatorsMetadata(ctx, syncBatch.After)
	if err != nil {
		return fmt.Errorf("update validators metadata: %w", err)
	}

	// Only log if there were actual changes
	if len(changedMetadata) > 0 {
		c.logger.Debug("validators metadata updated",
			zap.Int("total_in_batch", len(syncBatch.After)),
			zap.Int("actually_changed", len(changedMetadata)))
	}

	return nil
}

func (c *controller) reportIndicesChange(ctx context.Context) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*c.networkConfig.GetSlotDuration())
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return false
	case c.indicesChangeCh <- struct{}{}:
		return true
	}
}

func (c *controller) ReportValidatorStatuses(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			start := time.Now()
			validatorsPerStatus := make(map[validatorStatus]uint32)

			// Use ValidatorStore to get operator's validators
			for _, snapshot := range c.validatorStore.GetSelfValidators() {
				share := &snapshot.Share
				if share.IsParticipating(c.networkConfig, c.networkConfig.EstimatedCurrentEpoch()) {
					validatorsPerStatus[statusParticipating]++
				}
				if !share.HasBeaconMetadata() {
					validatorsPerStatus[statusNotFound]++
				} else if share.IsActive() {
					validatorsPerStatus[statusActive]++
				} else if share.Slashed() {
					validatorsPerStatus[statusSlashed]++
				} else if share.Exited() {
					validatorsPerStatus[statusExiting]++
				} else if !share.Activated() {
					validatorsPerStatus[statusNotActivated]++
				} else if share.Pending() {
					validatorsPerStatus[statusPending]++
				} else if share.ValidatorIndex == 0 {
					validatorsPerStatus[statusNoIndex]++
				} else {
					validatorsPerStatus[statusUnknown]++
				}
			}
			for status, count := range validatorsPerStatus {
				c.logger.
					With(zap.String("status", string(status))).
					With(zap.Uint32("count", count)).
					With(zap.Duration("elapsed_time", time.Since(start))).
					Info("recording validator status")
				recordValidatorStatus(ctx, count, status)
			}
		case <-ctx.Done():
			c.logger.Info("stopped reporting validator statuses. Context cancelled")
			return
		}
	}

}

func (c *controller) StopValidator(pubKey spectypes.ValidatorPK) error {
	logger := c.logger.Named("TaskExecutor").With(
		zap.String("task", "StopValidator"),
		fields.PubKey(pubKey[:]))

	// ValidatorStore will handle the removal and trigger OnValidatorStopped callback
	// which will call onShareStop asynchronously
	opts := registrystorage.UpdateOptions{
		TriggerCallbacks: true,
	}
	err := c.validatorStore.OnShareRemoved(c.ctx, pubKey, opts)
	if err != nil {
		logger.Error("failed to remove validator", zap.Error(err))
		return err
	}

	validatorsRemovedCounter.Add(c.ctx, 1)
	logger.Info("validator removal initiated")
	return nil
}

func (c *controller) ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex, ownValidator bool) error {
	logger := c.logger.Named("TaskExecutor").With(
		zap.String("task", "ExitValidator"),
		fields.PubKey(pubKey[:]),
		fields.BlockNumber(blockNumber),
		zap.Uint64("validator_index", uint64(validatorIndex)),
		zap.Bool("own_validator", ownValidator))

	// ValidatorStore will handle the exit and trigger OnValidatorExited callback
	var pk spectypes.ValidatorPK
	copy(pk[:], pubKey[:])

	opts := registrystorage.UpdateOptions{
		TriggerCallbacks: true,
	}
	err := c.validatorStore.OnValidatorExited(c.ctx, pk, blockNumber, opts)
	if err != nil {
		logger.Error("failed to process validator exit", zap.Error(err))
		return err
	}

	logger.Info("validator exit initiated")
	return nil
}

func SetupCommitteeRunners(
	ctx context.Context,
	options validator.Options,
) validator.CommitteeRunnerFunc {
	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner: options.Signer,
			Domain:       options.NetworkConfig.GetDomainType(),
			ValueCheckF:  valueCheckF,
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := qbft.RoundRobinProposer(state, round)
				return leader
			},
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig, role, nil),
			CutOffRound: roundtimer.CutOffRound,
		}

		identifier := spectypes.NewMsgID(options.NetworkConfig.GetDomainType(), options.Operator.CommitteeID[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.OperatorSigner, options.FullNode)
		return qbftCtrl
	}

	return func(
		slot phase0.Slot,
		shares map[phase0.ValidatorIndex]*spectypes.Share,
		attestingValidators []phase0.BLSPubKey,
		dutyGuard runner.CommitteeDutyGuard,
	) (*runner.CommitteeRunner, error) {
		// Create a committee runner.
		epoch := options.NetworkConfig.EstimatedEpochAtSlot(slot)
		valCheck := ssv.BeaconVoteValueCheckF(options.Signer, slot, attestingValidators, epoch)
		crunner, err := runner.NewCommitteeRunner(
			options.NetworkConfig,
			shares,
			buildController(spectypes.RoleCommittee, valCheck),
			options.Beacon,
			options.Network,
			options.Signer,
			options.OperatorSigner,
			valCheck,
			dutyGuard,
			options.DoppelgangerHandler,
		)
		if err != nil {
			return nil, err
		}
		return crunner.(*runner.CommitteeRunner), nil
	}
}

// SetupRunners initializes duty runners for the given validator
func SetupRunners(
	ctx context.Context,
	logger *zap.Logger,
	options validator.Options,
) (runner.ValidatorDutyRunners, error) {
	if options.SSVShare == nil || !options.SSVShare.HasBeaconMetadata() {
		logger.Error("missing validator metadata", zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey[:])))
		return runner.ValidatorDutyRunners{}, nil // TODO need to find better way to fix it
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
			Domain:       options.NetworkConfig.GetDomainType(),
			ValueCheckF:  nil, // sets per role type
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := qbft.RoundRobinProposer(state, round)
				//logger.Debug("leader", zap.Int("operator_id", int(leader)))
				return leader
			},
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig, role, nil),
			CutOffRound: roundtimer.CutOffRound,
		}
		config.ValueCheckF = valueCheckF

		identifier := spectypes.NewMsgID(options.NetworkConfig.GetDomainType(), options.SSVShare.ValidatorPubKey[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], options.Operator, config, options.OperatorSigner, options.FullNode)
		return qbftCtrl
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share) // TODO: fill the map
	shareMap[options.SSVShare.ValidatorIndex] = &options.SSVShare.Share

	runners := runner.ValidatorDutyRunners{}
	var err error
	for _, role := range runnersType {
		switch role {
		case spectypes.RoleProposer:
			proposedValueCheck := ssv.ProposerValueCheckF(options.Signer, options.NetworkConfig, options.SSVShare.ValidatorPubKey, options.SSVShare.ValidatorIndex, phase0.BLSPubKey(options.SSVShare.SharePubKey))
			qbftCtrl := buildController(spectypes.RoleProposer, proposedValueCheck)
			runners[role], err = runner.NewProposerRunner(logger, options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, options.DoppelgangerHandler, proposedValueCheck, 0, options.Graffiti, options.ProposerDelay)
		case spectypes.RoleAggregator:
			aggregatorValueCheckF := ssv.AggregatorValueCheckF(options.Signer, options.NetworkConfig, options.SSVShare.ValidatorPubKey, options.SSVShare.ValidatorIndex)
			qbftCtrl := buildController(spectypes.RoleAggregator, aggregatorValueCheckF)
			runners[role], err = runner.NewAggregatorRunner(options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, aggregatorValueCheckF, 0)
		case spectypes.RoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := ssv.SyncCommitteeContributionValueCheckF(options.Signer, options.NetworkConfig, options.SSVShare.ValidatorPubKey, options.SSVShare.ValidatorIndex)
			qbftCtrl := buildController(spectypes.RoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role], err = runner.NewSyncCommitteeAggregatorRunner(options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeContributionValueCheckF, 0)
		case spectypes.RoleValidatorRegistration:
			runners[role], err = runner.NewValidatorRegistrationRunner(options.NetworkConfig, shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner, options.GasLimit)
		case spectypes.RoleVoluntaryExit:
			runners[role], err = runner.NewVoluntaryExitRunner(options.NetworkConfig, shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		}
		if err != nil {
			return nil, errors.Wrap(err, "could not create duty runner")
		}
	}
	return runners, nil
}
