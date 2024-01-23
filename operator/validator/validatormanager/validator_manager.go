package validatormanager

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/networkconfig"
	operatordata "github.com/bloxapp/ssv/operator/operatordatastore"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator/metadatamanager"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

// ShareEventHandlerFunc is a function that handles event in an extended mode
type ShareEventHandlerFunc func(share *ssvtypes.SSVShare)

// ControllerOptions for creating a validator controller
// TODO: try to refactor this
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.Database
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	MetadataUpdateInterval     time.Duration `yaml:"MetadataUpdateInterval" env:"METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"Interval for updating metadata"`
	HistorySyncBatchSize       int           `yaml:"HistorySyncBatchSize" env:"HISTORY_SYNC_BATCH_SIZE" env-default:"25" env-description:"Maximum number of messages to sync in a single batch"`
	MinPeers                   int           `yaml:"MinimumPeers" env:"MINIMUM_PEERS" env-default:"2" env-description:"The required minimum peers for sync"`
	BeaconNetwork              beaconprotocol.Network
	Network                    network.P2PNetwork
	Beacon                     beaconprotocol.BeaconNode
	ShareEncryptionKeyProvider ShareEncryptionKeyProvider
	FullNode                   bool `yaml:"FullNode" env:"FULLNODE" env-default:"false" env-description:"Save decided history rather than just highest messages"`
	Exporter                   bool `yaml:"Exporter" env:"EXPORTER" env-default:"false" env-description:""`
	BuilderProposals           bool `yaml:"BuilderProposals" env:"BUILDER_PROPOSALS" env-default:"false" env-description:"Use external builders to produce blocks"`
	KeyManager                 spectypes.KeyManager
	OperatorDataStore          operatordata.OperatorDataStore
	RegistryStorage            nodestorage.Storage
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

type ValidatorManager struct {
	logger                    *zap.Logger
	metrics                   validator.Metrics
	context                   context.Context // TODO: get rid of passing this
	networkConfig             networkconfig.NetworkConfig
	p2pNetwork                network.P2PNetwork
	validatorsMap             *validatorsmap.ValidatorsMap
	nodeStorage               nodestorage.Storage
	operatorDataStore         operatordata.OperatorDataStore
	metadataManager           *metadatamanager.MetadataManager
	recentlyStartedValidators atomic.Uint64
	indicesChangeCh           chan struct{}
	validatorOptions          validator.Options // TODO: try to refactor
	nonCommitteeValidators    *ttlcache.Cache[spectypes.MessageID, *nonCommitteeValidator]
	nonCommitteeMutex         sync.Mutex
}

func New(
	ctx context.Context,
	logger *zap.Logger,
	metrics validator.Metrics,
	networkConfig networkconfig.NetworkConfig,
	p2pNetwork network.P2PNetwork,
	validatorsMap *validatorsmap.ValidatorsMap,
	nodeStorage nodestorage.Storage,
	operatorDataStore operatordata.OperatorDataStore,
	metadataManager *metadatamanager.MetadataManager,
	indicesChangeCh chan struct{},
	options ControllerOptions,
) *ValidatorManager {

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
		BuilderProposals:  options.BuilderProposals,
		GasLimit:          options.GasLimit,
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

	return &ValidatorManager{
		logger:            logger,
		metrics:           metrics,
		context:           ctx,
		networkConfig:     networkConfig,
		p2pNetwork:        p2pNetwork,
		validatorsMap:     validatorsMap,
		nodeStorage:       nodeStorage,
		operatorDataStore: operatorDataStore,
		metadataManager:   metadataManager,
		indicesChangeCh:   indicesChangeCh,
		validatorOptions:  validatorOptions,
		nonCommitteeValidators: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, *nonCommitteeValidator](time.Minute * 13),
		),
	}
}

func (vm *ValidatorManager) StartBackgroundCleanup() {
	go vm.nonCommitteeValidators.Start()
}

// StartPersistedValidators loads all persisted shares and set up the corresponding validators
func (vm *ValidatorManager) StartPersistedValidators() {
	if vm.validatorOptions.Exporter {
		vm.setupNonCommitteeValidators()
		return
	}

	shares := vm.nodeStorage.Shares().List(nil, registrystorage.ByNotLiquidated())
	if len(shares) == 0 {
		vm.logger.Info("could not find validators")
		return
	}

	var ownShares []*ssvtypes.SSVShare
	var allPubKeys = make([]spectypes.ValidatorPK, 0, len(shares))
	ownOpID := vm.operatorDataStore.GetOperatorData().ID
	for _, share := range shares {
		if share.BelongsToOperator(ownOpID) {
			ownShares = append(ownShares, share)
		}
		allPubKeys = append(allPubKeys, share.ValidatorPubKey)
	}

	// Start own validators.
	startedValidators := vm.setupValidatorsWithMetadata(ownShares)
	if startedValidators == 0 {
		// If no validators were started and therefore we're not subscribed to any subnets,
		// then subscribe to a random subnet to participate in the network.
		if err := vm.p2pNetwork.SubscribeRandoms(vm.logger, 1); err != nil {
			vm.logger.Error("failed to subscribe to random subnets", zap.Error(err))
		}
	}

	// Fetch metadata for all validators.
	start := time.Now()
	results, err := vm.metadataManager.UpdateValidatorsMetadataForPublicKeys(allPubKeys)
	if err != nil {
		vm.logger.Error("failed to update validators metadata after setup",
			zap.Int("shares", len(allPubKeys)),
			fields.Took(time.Since(start)),
			zap.Error(err))
		return
	}

	if err := vm.startValidatorsAfterMetadataUpdate(results); err != nil {
		vm.logger.Error("failed to start validators after setup",
			zap.Int("shares", len(allPubKeys)),
			fields.Took(time.Since(start)),
			zap.Error(err))
		return
	}

	vm.logger.Debug("updated validators metadata after setup",
		zap.Int("shares", len(allPubKeys)),
		fields.Took(time.Since(start)))
}

func (vm *ValidatorManager) CreateValidator(share *ssvtypes.SSVShare) (bool, error) {
	if !share.HasBeaconMetadata() { // fetching index and status in case not exist
		vm.logger.Warn("skipping validator until it becomes active", fields.PubKey(share.ValidatorPubKey))
		return false, nil
	}

	if err := vm.setShareFeeRecipient(share); err != nil {
		return false, fmt.Errorf("could not set share fee recipient: %w", err)
	}

	// Start a committee validator.
	v, found := vm.validatorsMap.GetValidator(share.ValidatorPubKey)
	if !found {
		if !share.HasBeaconMetadata() {
			return false, fmt.Errorf("beacon metadata is missing")
		}

		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(vm.context)

		opts := vm.prepareValidatorOptions(share, ctx)

		v = validator.NewValidator(ctx, cancel, opts)
		vm.validatorsMap.CreateValidator(share.ValidatorPubKey, v)

		vm.printShare(share, "setup validator done")

	} else {
		vm.printShare(v.Share, "get validator")
	}

	return vm.StartValidator(v)
}

func (vm *ValidatorManager) RemoveValidator(pubKey spectypes.ValidatorPK) {
	v, ok := vm.validatorsMap.GetValidator(pubKey)
	if ok {
		vm.validatorsMap.RemoveValidator(pubKey)
		v.Stop()
	}
}

// StartValidator will start the given validator if applicable
func (vm *ValidatorManager) StartValidator(v *validator.Validator) (bool, error) {
	vm.reportValidatorStatus(v.Share.ValidatorPubKey, v.Share.BeaconMetadata)
	if v.Share.BeaconMetadata.Index == 0 {
		return false, fmt.Errorf("could not start validator: index not found")
	}
	started, err := v.Start(vm.logger)
	if err != nil {
		vm.metrics.ValidatorError(v.Share.ValidatorPubKey)
		return false, fmt.Errorf("could not start validator: %w", err)
	}
	if started {
		vm.recentlyStartedValidators.Add(1)
	}
	return true, nil
}

// setupValidatorsWithMetadata setup and starts validators from the given shares,
// shares w/o validator's metadata won't start, but the metadata will be fetched and the validator will start afterwards
func (vm *ValidatorManager) setupValidatorsWithMetadata(shares []*ssvtypes.SSVShare) (started int) {
	vm.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		isStarted, err := vm.CreateValidator(validatorShare)
		if err != nil {
			vm.logger.Warn("could not start validator", fields.PubKey(validatorShare.ValidatorPubKey), zap.Error(err))
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
	vm.logger.Info("setup validators done", zap.Int("map size", vm.validatorsMap.Size()),
		zap.Int("failures", len(errs)), zap.Int("missing_metadata", len(fetchMetadata)),
		zap.Int("shares", len(shares)), zap.Int("started", started))
	return
}

// setupNonCommitteeValidators trigger SyncHighestDecided for each validator
// to start consensus flow which would save the highest decided instance
// and sync any gaps (in protocol/v2/qbft/controller/decided.go).
func (vm *ValidatorManager) setupNonCommitteeValidators() {
	nonCommitteeShares := vm.nodeStorage.Shares().List(nil, registrystorage.ByNotLiquidated())
	if len(nonCommitteeShares) == 0 {
		vm.logger.Info("could not find non-committee validators")
		return
	}

	pubKeys := make([]spectypes.ValidatorPK, 0, len(nonCommitteeShares))
	for _, validatorShare := range nonCommitteeShares {
		pubKeys = append(pubKeys, validatorShare.ValidatorPubKey)
	}
	if len(pubKeys) > 0 {
		vm.logger.Debug("updating metadata for non-committee validators", zap.Int("count", len(pubKeys)))
		results, err := vm.metadataManager.UpdateValidatorsMetadataForPublicKeys(pubKeys)
		if err != nil {
			vm.logger.Warn("could not update all validators", zap.Error(err))
			return
		}

		if err := vm.startValidatorsAfterMetadataUpdate(results); err != nil {
			vm.logger.Error("failed to start validators after setup",
				zap.Error(err))
			return
		}
	}
}

func (vm *ValidatorManager) setShareFeeRecipient(share *ssvtypes.SSVShare) error {
	data, found, err := vm.nodeStorage.GetRecipientData(nil, share.OwnerAddress)
	if err != nil {
		return fmt.Errorf("could not get recipient data: %w", err)
	}

	var feeRecipient bellatrix.ExecutionAddress
	if !found {
		vm.logger.Debug("setting fee recipient to owner address",
			fields.Validator(share.ValidatorPubKey), fields.FeeRecipient(share.OwnerAddress.Bytes()))
		copy(feeRecipient[:], share.OwnerAddress.Bytes())
	} else {
		vm.logger.Debug("setting fee recipient to storage data",
			fields.Validator(share.ValidatorPubKey), fields.FeeRecipient(data.FeeRecipient[:]))
		feeRecipient = data.FeeRecipient
	}
	share.SetFeeRecipient(feeRecipient)

	return nil
}

func (vm *ValidatorManager) printShare(s *ssvtypes.SSVShare, msg string) {
	committee := make([]string, len(s.Committee))
	for i, c := range s.Committee {
		committee[i] = fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.OperatorID, c.PubKey)
	}
	vm.logger.Debug(msg,
		fields.PubKey(s.ValidatorPubKey),
		zap.Uint64("node_id", s.OperatorID),
		zap.Strings("committee", committee),
		fields.FeeRecipient(s.FeeRecipientAddress[:]),
	)
}

// SetupRunners initializes duty runners for the given validator
func (vm *ValidatorManager) SetupRunners(ctx context.Context, options validator.Options) runner.DutyRunners {
	if options.SSVShare == nil || options.SSVShare.BeaconMetadata == nil {
		vm.logger.Error("missing validator metadata", zap.String("validator", hex.EncodeToString(options.SSVShare.ValidatorPubKey)))
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

	buildController := func(role spectypes.BeaconRole, valueCheckF specqbft.ProposedValueCheckF) *qbftcontroller.Controller {
		config := &qbft.Config{
			Signer:      options.Signer,
			SigningPK:   options.SSVShare.ValidatorPubKey, // TODO right val?
			Domain:      vm.networkConfig.Domain,
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

		identifier := spectypes.NewMsgID(vm.networkConfig.Domain, options.SSVShare.Share.ValidatorPubKey, role)
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
			runners[role].(*runner.ProposerRunner).ProducesBlindedBlocks = options.BuilderProposals // apply blinded block flag
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

func (vm *ValidatorManager) prepareValidatorOptions(share *ssvtypes.SSVShare, ctx context.Context) validator.Options {
	opts := vm.validatorOptions
	opts.SSVShare = share
	opts.DutyRunners = vm.SetupRunners(ctx, opts)
	return opts
}

func (vm *ValidatorManager) startValidatorAfterMetadataUpdate(share *ssvtypes.SSVShare, metadata *beaconprotocol.ValidatorMetadata) error {
	logger := vm.logger.With(zap.String("pk", hex.EncodeToString(share.ValidatorPubKey)))

	if v, found := vm.validatorsMap.GetValidator(share.ValidatorPubKey); found {
		if !v.Share.BeaconMetadata.Equals(metadata) {
			v.Share.BeaconMetadata = metadata
			logger.Debug("metadata was updated")
		}
		_, err := vm.StartValidator(v)
		if err != nil {
			logger.Warn("could not start validator after metadata update",
				zap.Error(err), zap.Any("metadata", metadata))
		}
	} else {
		logger.Info("starting new validator")

		started, err := vm.CreateValidator(share)
		if err != nil {
			return fmt.Errorf("could not start validator: %w", err)
		}
		if started {
			logger.Debug("started share after metadata update", zap.Bool("started", started))
		}
	}

	return nil
}
func (vm *ValidatorManager) startValidatorsAfterMetadataUpdate(results map[phase0.BLSPubKey]*beaconprotocol.ValidatorMetadata) error {
	var errs []error
	for pk, metadata := range results {
		// If this validator is not ours, don't start it.
		share := vm.nodeStorage.Shares().Get(nil, pk[:])
		if share == nil {
			return fmt.Errorf("share was not found")
		}
		if !share.BelongsToOperator(vm.operatorDataStore.GetOperatorData().ID) {
			return nil
		}

		if err := vm.startValidatorAfterMetadataUpdate(share, metadata); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		// TODO: update log
		vm.logger.Error("❌ failed to process validators returned from Beacon node",
			zap.Int("count", len(errs)), zap.Errors("errors", errs))
		return errors.Errorf("could not process %d validators returned from beacon", len(errs))
	}

	return nil
}

// UpdateValidatorMetaDataLoop triggers validator metadata update in an interval.
func (vm *ValidatorManager) UpdateValidatorMetaDataLoop() {
	interval := 2 * vm.networkConfig.SlotDurationSec()

	for {
		time.Sleep(interval)
		start := time.Now()

		vm.recentlyStartedValidators.Store(0)
		results, err := vm.metadataManager.UpdateValidatorMetaDataIteration()
		if err != nil {
			vm.logger.Warn("failed to update validators metadata", zap.Error(err))
			continue
		}

		if results == nil {
			continue
		}

		if err := vm.startValidatorsAfterMetadataUpdate(results); err != nil {
			vm.logger.Error("failed to start validators after triggering metadata update",
				zap.Error(err))
			return
		}

		recentlyStartedValidators := vm.recentlyStartedValidators.Load()

		vm.logger.Debug("started validators after metadata update",
			zap.Uint64("started_validators", recentlyStartedValidators),
			fields.Took(time.Since(start)))

		// Notify DutyScheduler of new validators.
		if recentlyStartedValidators > 0 {
			select {
			case vm.indicesChangeCh <- struct{}{}:
			case <-time.After(interval):
				vm.logger.Warn("timed out while notifying DutyScheduler of new validators")
			}
		}
	}
}
