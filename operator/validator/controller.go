package validator

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
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
	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

const (
	networkRouterConcurrency = 2048
)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *ssvtypes.SSVShare)

// ControllerOptions for creating a validator controller
type ControllerOptions struct {
	Context                        context.Context
	DB                             basedb.Database
	SignatureCollectionTimeout     time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval         time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating validator metadata"` // used outside of validator controller, left for compatibility
	HistorySyncBatchSize           int           `yaml:"HistorySyncBatchSize" env:"HISTORY_SYNC_BATCH_SIZE" env-default:"25" env-description:"Maximum number of messages to sync in a single batch"`
	MinPeers                       int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"Minimum number of peers required for sync"`
	Network                        P2PNetwork
	Beacon                         beaconprotocol.BeaconNode
	FullNode                       bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Store complete message history instead of just latest messages"`
	BeaconSigner                   ekm.BeaconSigner
	OperatorSigner                 ssvtypes.OperatorSigner
	OperatorDataStore              operatordatastore.OperatorDataStore
	RegistryStorage                nodestorage.Storage
	ValidatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter
	NewDecidedHandler              qbftcontroller.NewDecidedHandler
	DutyRoles                      []spectypes.BeaconRole
	DutyTraceCollector             *dutytracer.Collector
	StorageMap                     *storage.ParticipantStores
	ValidatorStore                 registrystorage.ValidatorStore
	MessageValidator               validation.MessageValidator
	ValidatorsMap                  *validators.ValidatorsMap
	DoppelgangerHandler            doppelganger.Provider
	NetworkConfig                  *networkconfig.Network
	ValidatorSyncer                *metadata.Syncer
	Graffiti                       []byte
	ProposerDelay                  time.Duration

	// worker flags
	WorkersCount    int    `yaml:"MsgWorkersCount" env:"MSG_WORKERS_COUNT" env-default:"256" env-description:"Number of message processing workers"`
	QueueBufferSize int    `yaml:"MsgWorkerBufferSize" env:"MSG_WORKER_BUFFER_SIZE" env-default:"65536" env-description:"Size of message worker queue buffer"`
	GasLimit        uint64 `yaml:"ExperimentalGasLimit" env:"EXPERIMENTAL_GAS_LIMIT" env-description:"Gas limit for MEV block proposals (must match across committee, otherwise MEV fails). Do not change unless you know what you're doing"`
}

type Nonce uint16

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

// Controller manages SSV node validators (their shares).
type Controller struct {
	ctx context.Context

	logger *zap.Logger

	networkConfig                  *networkconfig.Network
	sharesStorage                  SharesStorage
	operatorsStorage               registrystorage.Operators
	validatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter
	ibftStorageMap                 *storage.ParticipantStores

	beacon         beaconprotocol.BeaconNode
	beaconSigner   ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	operatorDataStore operatordatastore.OperatorDataStore

	validatorCommonOpts     *validator.CommonOptions
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

	// committeesObservers is a cache of initialized committeeObserver instances
	committeesObservers      *ttlcache.Cache[spectypes.MessageID, *validator.CommitteeObserver]
	committeesObserversMutex sync.Mutex

	attesterRoots   *ttlcache.Cache[phase0.Root, struct{}]
	syncCommRoots   *ttlcache.Cache[phase0.Root, struct{}]
	beaconVoteRoots *ttlcache.Cache[validator.BeaconVoteCacheKey, struct{}]

	domainCache *validator.DomainCache

	indicesChangeCh         chan struct{}
	validatorRegistrationCh chan duties.RegistrationDescriptor
	validatorExitCh         chan duties.ExitDescriptor
	feeRecipientChangeCh    chan struct{}

	traceCollector *dutytracer.Collector
}

// NewController creates a new validator controller instance.
func NewController(logger *zap.Logger, options ControllerOptions, exporterOptions exporter.Options) *Controller {
	logger.Debug("setting up validator controller")

	// lookup in a map that holds all relevant operators
	operatorsIDs := &sync.Map{}

	workerCfg := &worker.Config{
		Ctx:          options.Context,
		WorkersCount: options.WorkersCount,
		Buffer:       options.QueueBufferSize,
	}

	validatorCommonOpts := validator.NewCommonOptions(
		options.NetworkConfig,
		options.Network,
		options.Beacon,
		options.StorageMap,
		options.BeaconSigner,
		options.OperatorSigner,
		options.DoppelgangerHandler,
		options.NewDecidedHandler,
		options.FullNode,
		exporterOptions,
		options.HistorySyncBatchSize,
		options.GasLimit,
		options.MessageValidator,
		options.Graffiti,
		options.ProposerDelay,
	)

	cacheTTL := 2 * options.NetworkConfig.EpochDuration() // #nosec G115

	ctrl := &Controller{
		logger:                         logger.Named(log.NameController),
		networkConfig:                  options.NetworkConfig,
		sharesStorage:                  options.RegistryStorage.Shares(),
		operatorsStorage:               options.RegistryStorage,
		validatorRegistrationSubmitter: options.ValidatorRegistrationSubmitter,
		ibftStorageMap:                 options.StorageMap,
		validatorStore:                 options.ValidatorStore,
		ctx:                            options.Context,
		beacon:                         options.Beacon,
		operatorDataStore:              options.OperatorDataStore,
		beaconSigner:                   options.BeaconSigner,
		operatorSigner:                 options.OperatorSigner,
		network:                        options.Network,
		traceCollector:                 options.DutyTraceCollector,

		validatorsMap:       options.ValidatorsMap,
		validatorCommonOpts: validatorCommonOpts,

		validatorSyncer: options.ValidatorSyncer,

		operatorsIDs: operatorsIDs,

		messageRouter:        newMessageRouter(logger),
		messageWorker:        worker.NewWorker(logger, workerCfg),
		historySyncBatchSize: options.HistorySyncBatchSize,

		committeesObservers: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *validator.CommitteeObserver](cacheTTL),
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
		validatorRegistrationCh: make(chan duties.RegistrationDescriptor),
		validatorExitCh:         make(chan duties.ExitDescriptor),
		feeRecipientChangeCh:    make(chan struct{}, 1),
		committeeValidatorSetup: make(chan struct{}, 1),
		dutyGuard:               validator.NewCommitteeDutyGuard(),

		messageValidator: options.MessageValidator,
	}

	// Start automatic expired item deletion in nonCommitteeValidators.
	go ctrl.committeesObservers.Start()
	// Delete old root and domain entries.
	go ctrl.attesterRoots.Start()
	go ctrl.syncCommRoots.Start()
	go ctrl.domainCache.Start()
	go ctrl.beaconVoteRoots.Start()

	return ctrl
}

func (c *Controller) IndicesChangeChan() chan struct{} {
	return c.indicesChangeCh
}

func (c *Controller) ValidatorRegistrationChan() <-chan duties.RegistrationDescriptor {
	return c.validatorRegistrationCh
}

func (c *Controller) ValidatorExitChan() <-chan duties.ExitDescriptor {
	return c.validatorExitCh
}

func (c *Controller) FeeRecipientChangeChan() <-chan struct{} {
	return c.feeRecipientChangeCh
}

// GetValidatorStats returns stats of validators, including the following:
//   - the amount of validators in the network
//   - the amount of active validators (i.e. not slashed or existed)
//   - the amount of validators assigned to this operator
func (c *Controller) GetValidatorStats() (uint64, uint64, uint64, error) {
	operatorShares := uint64(0)
	active, total := uint64(0), uint64(0)
	c.sharesStorage.Range(nil, func(s *ssvtypes.SSVShare) bool {
		if ok := s.BelongsToOperator(c.operatorDataStore.GetOperatorID()); ok {
			operatorShares++
		}
		if s.IsParticipating(c.networkConfig.Beacon, c.networkConfig.EstimatedCurrentEpoch()) {
			active++
		}
		total++
		return true
	})
	return total, active, operatorShares, nil
}

func (c *Controller) handleRouterMessages() {
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
					v.EnqueueMessage(ctx, m)
				} else if vc, ok := c.validatorsMap.GetCommittee(cid); ok {
					vc.EnqueueMessage(ctx, m)
				} else if c.validatorCommonOpts.ExporterOptions.Enabled {
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
	spectypes.RoleCommittee:  64,
	spectypes.RoleProposer:   4,
	spectypes.RoleAggregator: 4,
	//spectypes.BNRoleSyncCommittee:             4,
	spectypes.RoleSyncCommitteeContribution: 4,
}

func (c *Controller) handleWorkerMessages(ctx context.Context, msg network.DecodedSSVMessage) error {
	ssvMsg := msg.(*queue.SSVMessage)

	var ncv *validator.CommitteeObserver

	item := c.committeesObservers.Get(ssvMsg.GetID())
	if item == nil || item.Value() == nil {
		committeeObserverOptions := validator.CommitteeObserverOptions{
			Logger:            c.logger,
			BeaconConfig:      c.networkConfig.Beacon,
			ValidatorStore:    c.validatorStore,
			Network:           c.validatorCommonOpts.Network,
			Storage:           c.validatorCommonOpts.Storage,
			FullNode:          c.validatorCommonOpts.FullNode,
			OperatorSigner:    c.validatorCommonOpts.OperatorSigner,
			NewDecidedHandler: c.validatorCommonOpts.NewDecidedHandler,
			AttesterRoots:     c.attesterRoots,
			SyncCommRoots:     c.syncCommRoots,
			DomainCache:       c.domainCache,
			BeaconVoteRoots:   c.beaconVoteRoots,
		}

		ncv = validator.NewCommitteeObserver(ssvMsg.GetID(), committeeObserverOptions)

		ttlSlots := nonCommitteeValidatorTTLs[ssvMsg.MsgID.GetRoleType()]
		ttl := time.Duration(ttlSlots) * c.networkConfig.SlotDuration

		c.committeesObservers.Set(ssvMsg.GetID(), ncv, ttl)
	} else {
		ncv = item.Value()
	}

	if c.validatorCommonOpts.ExporterOptions.Mode == exporter.ModeArchive {
		// use new exporter functionality
		return c.traceCollector.Collect(c.ctx, ssvMsg, ncv.VerifySig)
	}

	// use old exporter functionality
	return c.handleNonCommitteeMessages(ctx, ssvMsg, ncv)
}

func (c *Controller) handleNonCommitteeMessages(
	ctx context.Context,
	msg *queue.SSVMessage,
	ncv *validator.CommitteeObserver,
) error {
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

		return ncv.SaveRoots(ctx, msg)
	}

	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return err
		}

		return ncv.ProcessMessage(msg)
	}

	return nil
}

// StartValidators loads all persisted shares and sets up the corresponding validators
func (c *Controller) StartValidators(ctx context.Context) error {
	// TODO: Pass context wherever the execution flow may be blocked.

	if c.validatorCommonOpts.ExporterOptions.Enabled {
		// There are no committee validators to set up.
		close(c.committeeValidatorSetup)
		return nil
	}

	init := func() ([]*validator.Validator, []*validator.Committee, error) {
		defer close(c.committeeValidatorSetup)

		// Load non-liquidated shares that belong to our own Operator.
		ownShares := c.sharesStorage.List(
			nil,
			registrystorage.ByNotLiquidated(),
			registrystorage.ByOperatorID(c.operatorDataStore.GetOperatorID()),
		)
		if len(ownShares) == 0 {
			c.logger.Info("no validators to start: no own non-liquidated validator shares found in DB")
			return nil, nil, nil
		}

		// Setup committee validators.
		validators, committees := c.setupValidators(ownShares)
		if len(validators) == 0 {
			return nil, nil, fmt.Errorf("none of %d validators were successfully initialized", len(ownShares))
		}

		return validators, committees, nil
	}

	// Initialize validators.
	validators, committees, err := init()
	if err != nil {
		return fmt.Errorf("init validators: %w", err)
	}
	if len(validators) == 0 {
		// If no validators were initialized - we're not subscribed to any subnets,
		// we have to subscribe to 1 random subnet to participate in the network.
		if err := c.network.SubscribeRandoms(1); err != nil {
			return fmt.Errorf("subscribe to random subnets: %w", err)
		}
		c.logger.Info("no validators to start, successfully subscribed to random subnet")
		return nil
	}

	// Start validators.
	started := c.startValidators(validators, committees)
	if started == 0 {
		return fmt.Errorf("none of %d validators were successfully started", len(validators))
	}
	return nil
}

// setupValidators initializes validators for the provided shares.
// Share w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterward.
func (c *Controller) setupValidators(shares []*ssvtypes.SSVShare) ([]*validator.Validator, []*validator.Committee) {
	c.logger.Info("initializing validators ...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	validators := make([]*validator.Validator, 0, len(shares))
	committees := make([]*validator.Committee, 0, len(shares))
	for _, validatorShare := range shares {
		var initialized bool
		v, vc, err := c.onShareInit(validatorShare)
		if err != nil {
			c.logger.Warn("could not initialize validator", fields.PubKey(validatorShare.ValidatorPubKey[:]), zap.Error(err))
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
	c.logger.Info(
		"validator initialization is done",
		zap.Int("validators_size", c.validatorsMap.SizeValidators()),
		zap.Int("committee_size", c.validatorsMap.SizeCommittees()),
		zap.Int("failures", len(errs)),
		zap.Int("missing_metadata", len(fetchMetadata)),
		zap.Int("shares", len(shares)),
		zap.Int("initialized", len(validators)),
	)
	return validators, committees
}

func (c *Controller) startValidators(validators []*validator.Validator, committees []*validator.Committee) int {
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

	c.logger.Info("start validators done", zap.Int("map size", c.validatorsMap.SizeValidators()),
		zap.Int("failures", len(errs)),
		zap.Int("shares", len(validators)), zap.Int("started", started))
	return started
}

// StartNetworkHandlers init msg worker that handles network messages
func (c *Controller) StartNetworkHandlers() {
	c.network.UseMessageRouter(c.messageRouter)
	for i := 0; i < networkRouterConcurrency; i++ {
		go c.handleRouterMessages()
	}
	c.messageWorker.UseHandler(c.handleWorkerMessages)
}

// startEligibleValidators starts validators that transitioned to eligible to start due to a metadata update.
func (c *Controller) startEligibleValidators(ctx context.Context, pubKeys []spectypes.ValidatorPK) (count int) {
	// Build a map for quick lookup to ensure only explicitly listed validators start.
	validatorsSet := make(map[spectypes.ValidatorPK]struct{}, len(pubKeys))
	for _, v := range pubKeys {
		validatorsSet[v] = struct{}{}
	}

	// Filtering shares again ensures:
	// 1. Validators still exist (not removed or liquidated).
	// 2. They belong to this operator (ownership check).

	// Note: A validator might be removed from storage after being fetched but before starting.
	// In this case, it could still be added to validatorsMap despite no longer existing in sharesStorage,
	// leading to unnecessary tracking.
	operatorID := c.operatorDataStore.GetOperatorID()
	shares := c.sharesStorage.List(
		nil,
		registrystorage.ByOperatorID(operatorID),
		registrystorage.ByNotLiquidated(),
		func(share *ssvtypes.SSVShare) bool {
			_, exists := validatorsSet[share.ValidatorPubKey]
			return exists
		},
	)

	startedValidators := 0

	for _, share := range shares {
		select {
		case <-ctx.Done():
			c.logger.Warn("context canceled, stopping validator start loop")
			return startedValidators
		default:
		}

		// Start validator (if not already started).
		// TODO: why its in the map if not started?
		if v, found := c.validatorsMap.GetValidator(share.ValidatorPubKey); found {
			started, err := c.startValidator(v)
			if err != nil {
				c.logger.Warn("could not start validator", zap.Error(err))
			}
			if started {
				startedValidators++
			}
			vc, found := c.validatorsMap.GetCommittee(v.Share.CommitteeID())
			if found {
				vc.AddShare(&v.Share.Share)
			}
		} else {
			c.logger.Info("starting new validator", fields.PubKey(share.ValidatorPubKey[:]))

			started, err := c.onShareStart(share)
			if err != nil {
				c.logger.Warn("could not start newly active validator", zap.Error(err))
				continue
			}
			if started {
				startedValidators++
				c.logger.Debug("started share after metadata sync", zap.Bool("started", started))
			}
		}
	}

	return startedValidators
}

// GetValidator returns a validator instance from ValidatorsMap
func (c *Controller) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

func (c *Controller) ExecuteDuty(ctx context.Context, duty *spectypes.ValidatorDuty) {
	dutyEpoch := c.networkConfig.EstimatedEpochAtSlot(duty.Slot)
	dutyID := fields.BuildDutyID(c.networkConfig.EstimatedEpochAtSlot(duty.Slot), duty.Slot, duty.RunnerRole(), duty.ValidatorIndex)
	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconRoleAttribute(duty.Type),
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.BeaconEpochAttribute(dutyEpoch),
			observability.BeaconSlotAttribute(duty.Slot),
			observability.ValidatorPublicKeyAttribute(duty.PubKey),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	logger := c.logger.
		With(fields.RunnerRole(duty.RunnerRole())).
		With(fields.Epoch(dutyEpoch)).
		With(fields.Slot(duty.Slot)).
		With(fields.ValidatorIndex(duty.ValidatorIndex)).
		With(fields.Validator(duty.PubKey[:])).
		With(fields.DutyID(dutyID))

	v, ok := c.GetValidator(spectypes.ValidatorPK(duty.PubKey))
	if !ok {
		eventMsg := fmt.Sprintf("could not find validator: %s", duty.PubKey.String())
		logger.Warn(eventMsg)
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return
	}

	span.AddEvent("executing validator duty")
	if err := v.ExecuteDuty(ctx, duty); err != nil {
		logger.Error("could not execute validator duty", zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
}

func (c *Controller) ExecuteCommitteeDuty(ctx context.Context, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty) {
	cm, ok := c.validatorsMap.GetCommittee(committeeID)
	if !ok {
		const eventMsg = "could not find committee"
		c.logger.Warn(eventMsg, fields.CommitteeID(committeeID))
		return
	}

	committee := make([]spectypes.OperatorID, 0, len(cm.CommitteeMember.Committee))
	for _, operator := range cm.CommitteeMember.Committee {
		committee = append(committee, operator.OperatorID)
	}

	dutyEpoch := c.networkConfig.EstimatedEpochAtSlot(duty.Slot)
	dutyID := fields.BuildCommitteeDutyID(committee, dutyEpoch, duty.Slot)
	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "execute_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconEpochAttribute(dutyEpoch),
			observability.BeaconSlotAttribute(duty.Slot),
			observability.CommitteeIDAttribute(committeeID),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	logger := c.logger.
		With(fields.RunnerRole(duty.RunnerRole())).
		With(fields.Epoch(dutyEpoch)).
		With(fields.Slot(duty.Slot)).
		With(fields.CommitteeID(committeeID)).
		With(fields.DutyID(dutyID))

	span.AddEvent("executing committee duty")
	if err := cm.ExecuteDuty(ctx, duty); err != nil {
		logger.Error("could not execute committee duty", zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
}

func (c *Controller) FilterIndices(afterInit bool, filter func(*ssvtypes.SSVShare) bool) []phase0.ValidatorIndex {
	if afterInit {
		<-c.committeeValidatorSetup
	}
	var indices []phase0.ValidatorIndex
	c.sharesStorage.Range(nil, func(share *ssvtypes.SSVShare) bool {
		if filter(share) {
			indices = append(indices, share.ValidatorIndex)
		}
		return true
	})
	return indices
}

// onShareStop is called when a validator was removed or liquidated
func (c *Controller) onShareStop(pubKey spectypes.ValidatorPK) {
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
		}
	}
}

func (c *Controller) onShareInit(share *ssvtypes.SSVShare) (*validator.Validator, *validator.Committee, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		c.logger.Warn("skipping validator until it becomes active", fields.PubKey(share.ValidatorPubKey[:]))
		return nil, nil, nil
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

		dutyRunners, err := SetupRunners(validatorCtx, c.logger, share, operator, c.validatorRegistrationSubmitter, c.validatorStore, c.validatorCommonOpts)
		if err != nil {
			validatorCancel()
			return nil, nil, fmt.Errorf("could not setup runners: %w", err)
		}
		opts := c.validatorCommonOpts.NewOptions(share, operator, dutyRunners)

		v = validator.NewValidator(validatorCtx, validatorCancel, c.logger, opts)
		c.validatorsMap.PutValidator(share.ValidatorPubKey, v)

		c.printShare(share, "setup validator done")
	} else {
		c.printShare(v.Share, "get validator")
	}

	// Start a committee validator.
	vc, found := c.validatorsMap.GetCommittee(operator.CommitteeID)
	if !found {
		opts := c.validatorCommonOpts.NewOptions(share, operator, nil)
		committeeRunnerFunc := SetupCommitteeRunners(c.ctx, opts)

		vc = validator.NewCommittee(
			c.logger,
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

func (c *Controller) committeeMemberFromShare(share *ssvtypes.SSVShare) (*spectypes.CommitteeMember, error) {
	operators := make([]*spectypes.Operator, 0, len(share.Committee))
	var activeOperators uint64

	for _, cm := range share.Committee {
		opdata, found, err := c.operatorsStorage.GetOperatorData(nil, cm.Signer)
		if err != nil {
			return nil, fmt.Errorf("could not get operator data: %w", err)
		}

		operator := &spectypes.Operator{
			OperatorID: cm.Signer,
		}

		if !found {
			c.logger.Warn(
				"operator data not found, validator will only start if the number of available operators is greater than or equal to the committee quorum",
				fields.OperatorID(cm.Signer),
				fields.CommitteeID(share.CommitteeID()),
			)
		} else {
			activeOperators++

			operatorPEM, err := base64.StdEncoding.DecodeString(opdata.PublicKey)
			if err != nil {
				return nil, fmt.Errorf("could not decode public key: %w", err)
			}

			operator.SSVOperatorPubKey = operatorPEM
		}

		operators = append(operators, operator)
	}

	// This check is needed in case not all operators are available in storage.
	// It can happen after an operator is removed. In such a scenario, the committee should
	// continue conducting duties, but the number of operators must still meet the quorum.
	quorum, _ := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(share.Committee)))
	if activeOperators < quorum {
		return nil, fmt.Errorf("insufficient active operators for quorum: %d available, %d required", activeOperators, quorum)
	}

	faultyNodeTolerance := ssvtypes.ComputeF(uint64(len(share.Committee)))

	operatorPEM, err := base64.StdEncoding.DecodeString(c.operatorDataStore.GetOperatorData().PublicKey)
	if err != nil {
		return nil, fmt.Errorf("could not decode public key: %w", err)
	}

	return &spectypes.CommitteeMember{
		OperatorID:        c.operatorDataStore.GetOperatorID(),
		CommitteeID:       share.CommitteeID(),
		SSVOperatorPubKey: operatorPEM,
		FaultyNodes:       faultyNodeTolerance,
		Committee:         operators,
	}, nil
}

func (c *Controller) onShareStart(share *ssvtypes.SSVShare) (bool, error) {
	v, _, err := c.onShareInit(share)
	if err != nil || v == nil {
		return false, err
	}

	started, err := c.startValidator(v)
	if err != nil {
		return false, fmt.Errorf("start validator: %w", err)
	}

	return started, nil
}

func (c *Controller) printShare(s *ssvtypes.SSVShare, msg string) {
	committee := make([]string, len(s.Committee))
	for i, c := range s.Committee {
		committee[i] = fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.Signer, c.SharePubKey)
	}

	c.logger.Debug(msg,
		fields.PubKey(s.ValidatorPubKey[:]),
		zap.Bool("own_validator", s.BelongsToOperator(c.operatorDataStore.GetOperatorID())),
		zap.Strings("committee", committee),
	)
}

func (c *Controller) validatorStart(validator *validator.Validator) (bool, error) {
	if c.validatorStartFunc == nil {
		return validator.Start()
	}
	return c.validatorStartFunc(validator)
}

// startValidator will start the given validator if applicable
func (c *Controller) startValidator(v *validator.Validator) (bool, error) {
	c.reportValidatorStatus(v.Share)
	if v.Share.ValidatorIndex == 0 {
		return false, errors.New("validator index not found")
	}
	started, err := c.validatorStart(v)
	if err != nil {
		validatorErrorsCounter.Add(c.ctx, 1)
		return false, fmt.Errorf("could not start validator: %w", err)
	}

	return started, nil
}

func (c *Controller) HandleMetadataUpdates(ctx context.Context) {
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
func (c *Controller) handleMetadataUpdate(ctx context.Context, syncBatch metadata.SyncBatch) error {
	// Skip processing for full nodes (exporters) and operators that are still syncing
	// (i.e., haven't received their OperatorAdded event yet).
	if !c.operatorDataStore.OperatorIDReady() {
		return nil
	}

	// Identify validators that changed state (eligible to start, slashed, or exited) after the metadata update.
	eligibleToStart, slashedShares, exitedShares := syncBatch.DetectValidatorStateChanges()

	// Start only the validators that became eligible to start as a result of the metadata update.
	if len(eligibleToStart) > 0 || len(slashedShares) > 0 || len(exitedShares) > 0 {
		c.logger.Debug("validators state changed after metadata sync",
			zap.Int("eligible_to_start_count", len(eligibleToStart)),
			zap.Int("slashed_count", len(slashedShares)),
			zap.Int("exited_count", len(exitedShares)),
		)
	}

	if len(eligibleToStart) > 0 {
		startedValidators := c.startEligibleValidators(ctx, eligibleToStart)
		if startedValidators > 0 {
			c.logger.Debug("started new eligible validators", zap.Int("started_validators", startedValidators))

			// Notify duty scheduler about validator indices changes so the scheduler can update its duties
			if !c.reportIndicesChange(ctx) {
				c.logger.Error("failed to notify indices change")
			}
			// Notify fee recipient controller about validator changes due to metadata updates
			// so it can submit proposal preparations for the newly started validators
			if !c.reportFeeRecipientChange(ctx) {
				c.logger.Error("failed to notify fee recipient change")
			}
		} else {
			c.logger.Warn("no eligible validators started despite metadata changes")
		}
	}

	return nil
}

func (c *Controller) reportIndicesChange(ctx context.Context) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*c.networkConfig.SlotDuration)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return false
	case c.indicesChangeCh <- struct{}{}:
		return true
	}
}

func (c *Controller) reportFeeRecipientChange(ctx context.Context) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*c.networkConfig.SlotDuration)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return false
	case c.feeRecipientChangeCh <- struct{}{}:
		return true
	}
}

func (c *Controller) ReportValidatorStatuses(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			start := time.Now()
			validatorsPerStatus := make(map[validatorStatus]uint32)

			for _, share := range c.validatorStore.OperatorValidators(c.operatorDataStore.GetOperatorID()) {
				currentEpoch := c.networkConfig.EstimatedCurrentEpoch()
				if share.IsParticipating(c.networkConfig.Beacon, currentEpoch) {
					validatorsPerStatus[statusParticipating]++
				}
				if share.IsAttesting(currentEpoch) {
					validatorsPerStatus[statusAttesting]++
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
			c.logger.Info("stopped reporting validator statuses. Context canceled")
			return
		}
	}
}

func SetupCommitteeRunners(
	ctx context.Context,
	options *validator.Options,
) validator.CommitteeRunnerFunc {
	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner: options.Signer,
			Domain:       options.NetworkConfig.DomainType,
			ValueCheckF:  valueCheckF,
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := qbft.RoundRobinProposer(state, round)
				return leader
			},
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig.Beacon, role, nil),
			CutOffRound: roundtimer.CutOffRound,
		}

		identifier := spectypes.NewMsgID(options.NetworkConfig.DomainType, options.Operator.CommitteeID[:], role)
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
	share *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	validatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter,
	validatorStore registrystorage.ValidatorStore,
	options *validator.CommonOptions,
) (runner.ValidatorDutyRunners, error) {
	runnersType := []spectypes.RunnerRole{
		spectypes.RoleProposer,
		spectypes.RoleAggregator,
		spectypes.RoleSyncCommitteeContribution,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit,
	}

	buildController := func(role spectypes.RunnerRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			BeaconSigner: options.Signer,
			Domain:       options.NetworkConfig.DomainType,
			ValueCheckF:  nil, // is set per role type
			ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
				leader := qbft.RoundRobinProposer(state, round)
				return leader
			},
			Network:     options.Network,
			Timer:       roundtimer.New(ctx, options.NetworkConfig.Beacon, role, nil),
			CutOffRound: roundtimer.CutOffRound,
		}
		config.ValueCheckF = valueCheckF

		identifier := spectypes.NewMsgID(options.NetworkConfig.DomainType, share.ValidatorPubKey[:], role)
		qbftCtrl := qbftcontroller.NewController(identifier[:], operator, config, options.OperatorSigner, options.FullNode)
		return qbftCtrl
	}

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	shareMap[share.ValidatorIndex] = &share.Share

	runners := runner.ValidatorDutyRunners{}
	var err error
	for _, role := range runnersType {
		switch role {
		case spectypes.RoleProposer:
			proposedValueCheck := ssv.ProposerValueCheckF(options.Signer, options.NetworkConfig.Beacon, share.ValidatorPubKey, share.ValidatorIndex, phase0.BLSPubKey(share.SharePubKey))
			qbftCtrl := buildController(spectypes.RoleProposer, proposedValueCheck)
			runners[role], err = runner.NewProposerRunner(logger, options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, options.DoppelgangerHandler, proposedValueCheck, 0, options.Graffiti, options.ProposerDelay)
		case spectypes.RoleAggregator:
			aggregatorValueCheckF := ssv.AggregatorValueCheckF(options.Signer, options.NetworkConfig.Beacon, share.ValidatorPubKey, share.ValidatorIndex)
			qbftCtrl := buildController(spectypes.RoleAggregator, aggregatorValueCheckF)
			runners[role], err = runner.NewAggregatorRunner(options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, aggregatorValueCheckF, 0)
		case spectypes.RoleSyncCommitteeContribution:
			syncCommitteeContributionValueCheckF := ssv.SyncCommitteeContributionValueCheckF(options.Signer, options.NetworkConfig.Beacon, share.ValidatorPubKey, share.ValidatorIndex)
			qbftCtrl := buildController(spectypes.RoleSyncCommitteeContribution, syncCommitteeContributionValueCheckF)
			runners[role], err = runner.NewSyncCommitteeAggregatorRunner(options.NetworkConfig, shareMap, qbftCtrl, options.Beacon, options.Network, options.Signer, options.OperatorSigner, syncCommitteeContributionValueCheckF, 0)
		case spectypes.RoleValidatorRegistration:
			runners[role], err = runner.NewValidatorRegistrationRunner(options.NetworkConfig, shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner, validatorRegistrationSubmitter, validatorStore, options.GasLimit)
		case spectypes.RoleVoluntaryExit:
			runners[role], err = runner.NewVoluntaryExitRunner(options.NetworkConfig, shareMap, options.Beacon, options.Network, options.Signer, options.OperatorSigner)
		default:
			return nil, fmt.Errorf("unexpected duty runner type: %s", role)
		}
		if err != nil {
			return nil, fmt.Errorf("could not create duty runner: %w", err)
		}
	}
	return runners, nil
}
