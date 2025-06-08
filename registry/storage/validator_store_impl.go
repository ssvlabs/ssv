package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"

	"github.com/ssvlabs/ssv/operator/duties"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// validatorStoreImpl is the concrete implementation of ValidatorIndices.
// It manages all validator state transitions in a thread-safe manner.
type validatorStoreImpl struct {
	logger           *zap.Logger
	sharesStorage    Shares
	operatorsStorage Operators
	beaconCfg        networkconfig.Beacon
	operatorIDFn     func() spectypes.OperatorID

	mu         sync.RWMutex
	validators map[string]*validatorState
	committees map[spectypes.CommitteeID]*committeeState
	indices    map[phase0.ValidatorIndex]spectypes.ValidatorPK

	callbacks ValidatorLifecycleCallbacks

	syncCommittees map[uint64]map[phase0.ValidatorIndex][]phase0.CommitteeIndex
}

// validatorState holds the mutable state for a validator.
type validatorState struct {
	share               *types.SSVShare
	lastUpdated         time.Time
	participationStatus ParticipationStatus
}

// committeeState holds the mutable state for a committee.
type committeeState struct {
	id         spectypes.CommitteeID
	operators  []spectypes.OperatorID
	validators map[spectypes.ValidatorPK]struct{}
}

// NewValidatorStore creates a new ValidatorStore instance.
// It initializes the internal structures for managing validator states,
// committees, and indices.
func NewValidatorStore(
	logger *zap.Logger,
	sharesStorage Shares,
	operatorsStorage Operators,
	beaconCfg networkconfig.Beacon,
	operatorIDFn func() spectypes.OperatorID,
) (ValidatorStore, error) {
	s := &validatorStoreImpl{
		logger:           logger.Named("validator_store"),
		sharesStorage:    sharesStorage,
		operatorsStorage: operatorsStorage,
		beaconCfg:        beaconCfg,
		operatorIDFn:     operatorIDFn,
		validators:       make(map[string]*validatorState),
		committees:       make(map[spectypes.CommitteeID]*committeeState),
		indices:          make(map[phase0.ValidatorIndex]spectypes.ValidatorPK),
		syncCommittees:   make(map[uint64]map[phase0.ValidatorIndex][]phase0.CommitteeIndex),
	}

	shares := sharesStorage.List(nil)
	for _, share := range shares {
		state := &validatorState{
			share:               s.copyShare(share),
			lastUpdated:         time.Now(),
			participationStatus: s.calculateParticipationStatus(share),
		}

		key := hex.EncodeToString(share.ValidatorPubKey[:])
		s.validators[key] = state

		if share.HasBeaconMetadata() {
			s.indices[share.ValidatorIndex] = share.ValidatorPubKey
		}

		if err := s.addShareToCommittee(share); err != nil {
			return nil, fmt.Errorf("add share to committee: %w", err)
		}
	}

	logger.Info("validator store initialized",
		zap.Int("validators", len(s.validators)),
		zap.Int("committees", len(s.committees)))

	return s, nil
}

// RegisterLifecycleCallbacks sets the callbacks that will be invoked upon
// validator lifecycle events such as addition, removal, start, or stop.
// This method is protected by a mutex to ensure thread-safe updates to callbacks.
func (s *validatorStoreImpl) RegisterLifecycleCallbacks(callbacks ValidatorLifecycleCallbacks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks = callbacks
}

// OnShareAdded handles the addition of a new validator share to the store.
// It validates operators exist, creates an immutable copy of the share, updates internal state
// including validator maps, indices, and committee memberships, and persists to storage.
// If registered, OnValidatorAdded and OnValidatorStarted (if applicable) callbacks
// are triggered asynchronously.
// Returns an error if the share is nil, operators don't exist, or if the validator already exists.
func (s *validatorStoreImpl) OnShareAdded(ctx context.Context, share *types.SSVShare) error {
	if share == nil {
		return fmt.Errorf("nil share")
	}

	// Validate operators exist before acquiring lock
	operatorIDs := share.OperatorIDs()
	exist, err := s.operatorsStorage.OperatorsExist(nil, operatorIDs)
	if err != nil {
		return fmt.Errorf("check operators exist: %w", err)
	}
	if !exist {
		return fmt.Errorf("one or more operators don't exist")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := hex.EncodeToString(share.ValidatorPubKey[:])
	if _, exists := s.validators[key]; exists {
		return fmt.Errorf("validator already exists: %s", key)
	}

	shareCopy := s.copyShare(share)

	state := &validatorState{
		share:               shareCopy,
		lastUpdated:         time.Now(),
		participationStatus: s.calculateParticipationStatus(shareCopy),
	}

	s.validators[key] = state

	if share.HasBeaconMetadata() {
		s.indices[share.ValidatorIndex] = share.ValidatorPubKey
	}

	if err := s.addShareToCommittee(shareCopy); err != nil {
		delete(s.validators, key)
		if share.HasBeaconMetadata() {
			delete(s.indices, share.ValidatorIndex)
		}
		return fmt.Errorf("add to committee: %w", err)
	}

	// Persist to storage
	if err := s.sharesStorage.Save(nil, shareCopy); err != nil {
		delete(s.validators, key)
		if share.HasBeaconMetadata() {
			delete(s.indices, share.ValidatorIndex)
		}

		_ = s.removeShareFromCommittee(shareCopy)
		return fmt.Errorf("persist share: %w", err)
	}

	// Create snapshot for callbacks
	snapshot := s.createSnapshot(state)

	// Call lifecycle callback
	if s.callbacks.OnValidatorAdded != nil {
		go func() {
			if err := s.callbacks.OnValidatorAdded(ctx, snapshot); err != nil {
				s.logger.Error("validator added callback failed", zap.Error(err))
			}
		}()
	}

	// Check if should start
	if s.shouldStart(state) && s.callbacks.OnValidatorStarted != nil {
		go func() {
			if err := s.callbacks.OnValidatorStarted(ctx, snapshot); err != nil {
				s.logger.Error("validator started callback failed", zap.Error(err))
			}
		}()
	}

	return nil
}

// OnShareUpdated handles updates to an existing validator share.
// It updates the validator's state, including its participation status and beacon metadata,
// and persists changes to storage.
// Relevant lifecycle callbacks (OnValidatorUpdated, OnValidatorStarted, or OnValidatorStopped)
// are triggered based on the change in participation status.
// Returns an error if the share is nil or if the validator is not found.
func (s *validatorStoreImpl) OnShareUpdated(ctx context.Context, share *types.SSVShare) error {
	if share == nil {
		return fmt.Errorf("nil share")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := hex.EncodeToString(share.ValidatorPubKey[:])
	state, exists := s.validators[key]
	if !exists {
		return fmt.Errorf("validator not found: %s", key)
	}

	wasParticipating := s.shouldStart(state)
	shareCopy := s.copyShare(share)

	state.share = shareCopy
	state.lastUpdated = time.Now()
	state.participationStatus = s.calculateParticipationStatus(shareCopy)

	// Update indices if beacon metadata changed
	if share.HasBeaconMetadata() {
		s.indices[share.ValidatorIndex] = share.ValidatorPubKey
	}

	// Update committee if needed
	if err := s.updateShareInCommittee(shareCopy); err != nil {
		return fmt.Errorf("update committee: %w", err)
	}

	// Persist to storage
	if err := s.sharesStorage.Save(nil, shareCopy); err != nil {
		return fmt.Errorf("persist share: %w", err)
	}

	// Capture new participation state after update
	isParticipating := s.shouldStart(state)
	newSnapshot := s.createSnapshot(state)

	// Always trigger update callback
	if s.callbacks.OnValidatorUpdated != nil {
		go func() {
			if err := s.callbacks.OnValidatorUpdated(ctx, newSnapshot); err != nil {
				s.logger.Error("validator updated callback failed", zap.Error(err))
			}
		}()
	}

	// Handle participation state changes
	if !wasParticipating && isParticipating && s.callbacks.OnValidatorStarted != nil {
		go func() {
			if err := s.callbacks.OnValidatorStarted(ctx, newSnapshot); err != nil {
				s.logger.Error("validator started callback failed", zap.Error(err))
			}
		}()
	} else if wasParticipating && !isParticipating && s.callbacks.OnValidatorStopped != nil {
		go func() {
			if err := s.callbacks.OnValidatorStopped(ctx, share.ValidatorPubKey); err != nil {
				s.logger.Error("validator stopped callback failed", zap.Error(err))
			}
		}()
	}

	return nil
}

// OnShareRemoved handles the removal of a validator share from the store.
// It cleans up the validator's state from internal maps, indices, and committees,
// and removes it from storage.
// If registered, OnValidatorRemoved and OnValidatorStopped (if the validator was active)
// callbacks are triggered asynchronously.
// Returns an error if the validator is not found.
func (s *validatorStoreImpl) OnShareRemoved(ctx context.Context, pubKey spectypes.ValidatorPK) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := hex.EncodeToString(pubKey[:])
	state, exists := s.validators[key]
	if !exists {
		return fmt.Errorf("validator not found: %s", key)
	}

	wasParticipating := s.shouldStart(state)

	// Remove from indices
	if state.share.HasBeaconMetadata() {
		delete(s.indices, state.share.ValidatorIndex)
	}

	// Remove from committee
	if err := s.removeShareFromCommittee(state.share); err != nil {
		s.logger.Warn("failed to remove share from committee",
			zap.String("pubkey", hex.EncodeToString(pubKey[:])),
			zap.Error(err))
	}

	// Remove from validators map
	delete(s.validators, key)

	// Remove from storage
	if err := s.sharesStorage.Delete(nil, pubKey[:]); err != nil {
		// Log but don't fail - internal state already updated
		s.logger.Error("failed to delete share from storage",
			zap.String("pubkey", hex.EncodeToString(pubKey[:])),
			zap.Error(err))
	}

	// Call callbacks
	if s.callbacks.OnValidatorRemoved != nil {
		go func() {
			if err := s.callbacks.OnValidatorRemoved(ctx, pubKey); err != nil {
				s.logger.Error("validator removed callback failed", zap.Error(err))
			}
		}()
	}

	// If was participating, also call stop callback
	if wasParticipating && s.callbacks.OnValidatorStopped != nil {
		go func() {
			if err := s.callbacks.OnValidatorStopped(ctx, pubKey); err != nil {
				s.logger.Error("validator stopped callback failed", zap.Error(err))
			}
		}()
	}

	return nil
}

// OnClusterLiquidated handles the event of a cluster being liquidated.
// It identifies all validators belonging to the specified cluster (by owner and operator IDs),
// marks them as liquidated, updates their participation status, persists changes to storage,
// and triggers relevant OnValidatorStopped and OnValidatorUpdated callbacks.
func (s *validatorStoreImpl) OnClusterLiquidated(ctx context.Context, owner common.Address, operatorIDs []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	clusterID := types.ComputeClusterIDHash(owner, operatorIDs)

	var affectedValidators []*validatorState
	var affectedShares []*types.SSVShare
	for _, state := range s.validators {
		shareClusterID := types.ComputeClusterIDHash(state.share.OwnerAddress, state.share.OperatorIDs())
		if bytes.Equal(shareClusterID, clusterID) {
			affectedValidators = append(affectedValidators, state)
		}
	}

	if len(affectedValidators) == 0 {
		s.logger.Debug("no validators found for liquidated cluster",
			zap.String("owner", owner.Hex()),
			zap.Any("operators", operatorIDs))
		return nil
	}

	for _, state := range affectedValidators {
		wasParticipating := s.shouldStart(state)

		// Mark as liquidated
		state.share.Liquidated = true
		state.lastUpdated = time.Now()
		state.participationStatus = s.calculateParticipationStatus(state.share)

		affectedShares = append(affectedShares, state.share)

		pubKey := state.share.ValidatorPubKey

		// Trigger stop callback if was participating
		if wasParticipating && s.callbacks.OnValidatorStopped != nil {
			go func(pk spectypes.ValidatorPK) {
				if err := s.callbacks.OnValidatorStopped(ctx, pk); err != nil {
					s.logger.Error("validator stopped callback failed",
						zap.String("pubkey", hex.EncodeToString(pk[:])),
						zap.Error(err))
				}
			}(pubKey)
		}

		// Trigger updated callback
		if s.callbacks.OnValidatorUpdated != nil {
			snapshot := s.createSnapshot(state)
			go func(snap *ValidatorSnapshot) {
				if err := s.callbacks.OnValidatorUpdated(ctx, snap); err != nil {
					s.logger.Error("validator updated callback failed", zap.Error(err))
				}
			}(snapshot)
		}
	}

	// Persist all changes
	if err := s.sharesStorage.Save(nil, affectedShares...); err != nil {
		return fmt.Errorf("persist liquidated shares: %w", err)
	}

	s.logger.Info("cluster liquidated",
		zap.String("owner", owner.Hex()),
		zap.Any("operators", operatorIDs),
		zap.Int("affected_validators", len(affectedValidators)))

	return nil
}

// OnClusterReactivated handles the event of a cluster being reactivated.
// It identifies all validators belonging to the specified cluster, unmarks them
// as liquidated, updates their participation status, and triggers relevant
// OnValidatorStarted and OnValidatorUpdated callbacks.
func (s *validatorStoreImpl) OnClusterReactivated(ctx context.Context, owner common.Address, operatorIDs []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	clusterID := types.ComputeClusterIDHash(owner, operatorIDs)

	// Find all shares in this cluster
	var affectedValidators []*validatorState
	for _, state := range s.validators {
		shareClusterID := types.ComputeClusterIDHash(state.share.OwnerAddress, state.share.OperatorIDs())
		if bytes.Equal(shareClusterID, clusterID) {
			affectedValidators = append(affectedValidators, state)
		}
	}

	if len(affectedValidators) == 0 {
		s.logger.Debug("no validators found for reactivated cluster",
			zap.String("owner", owner.Hex()),
			zap.Any("operators", operatorIDs))
		return nil
	}

	for _, state := range affectedValidators {
		wasParticipating := s.shouldStart(state)

		state.share.Liquidated = false
		state.lastUpdated = time.Now()
		state.participationStatus = s.calculateParticipationStatus(state.share)

		isParticipating := s.shouldStart(state)

		// Trigger start callback if now participating
		if !wasParticipating && isParticipating && s.callbacks.OnValidatorStarted != nil {
			snapshot := s.createSnapshot(state)
			go func(snap *ValidatorSnapshot) {
				if err := s.callbacks.OnValidatorStarted(ctx, snap); err != nil {
					s.logger.Error("validator started callback failed", zap.Error(err))
				}
			}(snapshot)
		}

		// Trigger updated callback
		if s.callbacks.OnValidatorUpdated != nil {
			snapshot := s.createSnapshot(state)
			go func(snap *ValidatorSnapshot) {
				if err := s.callbacks.OnValidatorUpdated(ctx, snap); err != nil {
					s.logger.Error("validator updated callback failed", zap.Error(err))
				}
			}(snapshot)
		}
	}

	s.logger.Info("cluster reactivated",
		zap.String("owner", owner.Hex()),
		zap.Any("operators", operatorIDs),
		zap.Int("affected_validators", len(affectedValidators)))

	return nil
}

// OnOperatorRemoved handles the removal of an operator from the system.
// It identifies all validators associated with the removed operator, removes them
// from the store, and triggers OnValidatorRemoved and OnValidatorStopped callbacks.
func (s *validatorStoreImpl) OnOperatorRemoved(ctx context.Context, operatorID spectypes.OperatorID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var affectedValidators []*validatorState
	var affectedKeys []string

	for key, state := range s.validators {
		for _, member := range state.share.Committee {
			if member.Signer == operatorID {
				affectedValidators = append(affectedValidators, state)
				affectedKeys = append(affectedKeys, key)
				break
			}
		}
	}

	if len(affectedValidators) == 0 {
		s.logger.Debug("no validators affected by operator removal",
			zap.Uint64("operator_id", operatorID))
		return nil
	}

	for i, state := range affectedValidators {
		key := affectedKeys[i]
		wasParticipating := s.shouldStart(state)
		pubKey := state.share.ValidatorPubKey

		// Remove from indices
		if state.share.HasBeaconMetadata() {
			delete(s.indices, state.share.ValidatorIndex)
		}

		// Remove from committee
		if err := s.removeShareFromCommittee(state.share); err != nil {
			s.logger.Warn("failed to remove share from committee",
				zap.String("pubkey", hex.EncodeToString(pubKey[:])),
				zap.Error(err))
		}

		// Remove from validators map
		delete(s.validators, key)

		// Trigger callbacks
		if s.callbacks.OnValidatorRemoved != nil {
			go func(pk spectypes.ValidatorPK) {
				if err := s.callbacks.OnValidatorRemoved(ctx, pk); err != nil {
					s.logger.Error("validator removed callback failed",
						zap.String("pubkey", hex.EncodeToString(pk[:])),
						zap.Error(err))
				}
			}(pubKey)
		}

		// If was participating, also call stop callback
		if wasParticipating && s.callbacks.OnValidatorStopped != nil {
			go func(pk spectypes.ValidatorPK) {
				if err := s.callbacks.OnValidatorStopped(ctx, pk); err != nil {
					s.logger.Error("validator stopped callback failed",
						zap.String("pubkey", hex.EncodeToString(pk[:])),
						zap.Error(err))
				}
			}(pubKey)
		}
	}

	s.logger.Info("operator removed, validators affected",
		zap.Uint64("operator_id", operatorID),
		zap.Int("affected_validators", len(affectedValidators)))

	return nil
}

// OnFeeRecipientUpdated handles updates to a validator's fee recipient.
func (s *validatorStoreImpl) OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var affectedValidators []*validatorState
	for _, state := range s.validators {
		if state.share.OwnerAddress == owner {
			affectedValidators = append(affectedValidators, state)
		}
	}

	if len(affectedValidators) == 0 {
		s.logger.Debug("no validators found for fee recipient update",
			zap.String("owner", owner.Hex()))
		return nil
	}

	for _, state := range affectedValidators {
		// Update the fee recipient
		state.share.SetFeeRecipient(bellatrix.ExecutionAddress(recipient))
		state.lastUpdated = time.Now()

		// Trigger updated callback
		if s.callbacks.OnValidatorUpdated != nil {
			snapshot := s.createSnapshot(state)
			go func(snap *ValidatorSnapshot) {
				if err := s.callbacks.OnValidatorUpdated(ctx, snap); err != nil {
					s.logger.Error("validator updated callback failed", zap.Error(err))
				}
			}(snapshot)
		}
	}

	s.logger.Info("fee recipient updated",
		zap.String("owner", owner.Hex()),
		zap.String("recipient", recipient.Hex()),
		zap.Int("affected_validators", len(affectedValidators)))

	return nil
}

// OnValidatorExited handles the event of a validator initiating a voluntary exit.
// TODO: rethink it
func (s *validatorStoreImpl) OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := hex.EncodeToString(pubKey[:])
	state, exists := s.validators[key]
	if !exists {
		return fmt.Errorf("validator not found: %s", key)
	}

	// Trigger validator exited callback if exists
	if s.callbacks.OnValidatorExited != nil {
		exitDescriptor := duties.ExitDescriptor{
			PubKey:         phase0.BLSPubKey(pubKey),
			ValidatorIndex: state.share.ValidatorIndex,
			BlockNumber:    blockNumber,
			OwnValidator:   state.share.BelongsToOperator(s.operatorIDFn()),
		}
		go func(desc duties.ExitDescriptor) {
			if err := s.callbacks.OnValidatorExited(ctx, desc); err != nil {
				s.logger.Error("validator exited callback failed", zap.Error(err))
			}
		}(exitDescriptor)
	}

	s.logger.Info("validator exit initiated",
		zap.String("pubkey", hex.EncodeToString(pubKey[:])),
		zap.Uint64("block_number", blockNumber))

	return nil
}

// GetValidator returns a validator by either public key or index.
func (s *validatorStoreImpl) GetValidator(id ValidatorID) (*ValidatorSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var key string

	switch v := id.(type) {
	case ValidatorPubKey:
		key = hex.EncodeToString(v[:])
	case ValidatorIndex:
		pubKey, exists := s.indices[phase0.ValidatorIndex(v)]
		if !exists {
			return nil, false
		}
		key = hex.EncodeToString(pubKey[:])
	default:
		return nil, false
	}

	state, exists := s.validators[key]
	if !exists {
		return nil, false
	}

	return s.createSnapshot(state), true
}

// GetCommittee returns a committee snapshot by ID.
func (s *validatorStoreImpl) GetCommittee(id spectypes.CommitteeID) (*CommitteeSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	committee, exists := s.committees[id]
	if !exists {
		return nil, false
	}

	return s.createCommitteeSnapshot(committee), true
}

// GetAllValidators returns snapshots of all validators.
func (s *validatorStoreImpl) GetAllValidators() []*ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshots := make([]*ValidatorSnapshot, 0, len(s.validators))
	for _, state := range s.validators {
		snapshots = append(snapshots, s.createSnapshot(state))
	}

	return snapshots
}

// GetOperatorValidators returns validators belonging to a specific operator.
func (s *validatorStoreImpl) GetOperatorValidators(operatorID spectypes.OperatorID) []*ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snapshots []*ValidatorSnapshot
	for _, state := range s.validators {
		if state.share.BelongsToOperator(operatorID) {
			snapshots = append(snapshots, s.createSnapshot(state))
		}
	}

	return snapshots
}

// GetParticipatingValidators returns validators that are participating based on options.
func (s *validatorStoreImpl) GetParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snapshots []*ValidatorSnapshot
	for _, state := range s.validators {
		// Check liquidation filter
		if state.share.Liquidated && !opts.IncludeLiquidated {
			continue
		}

		// Check exit filter
		if state.share.Exited() && !opts.IncludeExited {
			continue
		}

		// Check attesting filter
		if opts.OnlyAttesting && !state.share.IsAttesting(epoch) {
			continue
		}

		// Check sync committee filter
		if opts.OnlySyncCommittee {
			period := s.beaconCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			if !s.isInSyncCommittee(state.share.ValidatorIndex, period) {
				continue
			}
		}

		// Check if participating (unless filtered out above)
		if state.participationStatus.IsParticipating || opts.IncludeLiquidated || opts.IncludeExited {
			snapshots = append(snapshots, s.createSnapshot(state))
		}
	}

	return snapshots
}

// GetCommittees returns all committees.
func (s *validatorStoreImpl) GetCommittees() []*CommitteeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshots := make([]*CommitteeSnapshot, 0, len(s.committees))
	for _, committee := range s.committees {
		snapshots = append(snapshots, s.createCommitteeSnapshot(committee))
	}

	return snapshots
}

// GetOperatorCommittees returns committees that include a specific operator.
func (s *validatorStoreImpl) GetOperatorCommittees(operatorID spectypes.OperatorID) []*CommitteeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snapshots []*CommitteeSnapshot
	for _, committee := range s.committees {
		for _, op := range committee.operators {
			if op == operatorID {
				snapshots = append(snapshots, s.createCommitteeSnapshot(committee))
				break
			}
		}
	}

	return snapshots
}

// RegisterSyncCommitteeInfo registers sync committee assignments.
func (s *validatorStoreImpl) RegisterSyncCommitteeInfo(info []SyncCommitteeInfo) error {
	if len(info) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sci := range info {
		if len(sci.Indices) == 0 {
			continue
		}

		// Ensure the period map exists
		if _, exists := s.syncCommittees[sci.Period]; !exists {
			s.syncCommittees[sci.Period] = make(map[phase0.ValidatorIndex][]phase0.CommitteeIndex)
		}

		// Store the committee indices for this validator
		s.syncCommittees[sci.Period][sci.ValidatorIndex] = sci.Indices

		// Update participation status for affected validator
		for _, state := range s.validators {
			if state.share.ValidatorIndex == sci.ValidatorIndex {
				oldStatus := state.participationStatus
				state.participationStatus = s.calculateParticipationStatus(state.share)

				// If participation changed, trigger callbacks
				if oldStatus.IsParticipating != state.participationStatus.IsParticipating {
					if state.participationStatus.IsParticipating && s.callbacks.OnValidatorStarted != nil {
						snapshot := s.createSnapshot(state)
						go func(snap *ValidatorSnapshot) {
							ctx := context.Background()
							if err := s.callbacks.OnValidatorStarted(ctx, snap); err != nil {
								s.logger.Error("validator started callback failed", zap.Error(err))
							}
						}(snapshot)
					} else if !state.participationStatus.IsParticipating && s.callbacks.OnValidatorStopped != nil {
						go func(pk spectypes.ValidatorPK) {
							ctx := context.Background()
							if err := s.callbacks.OnValidatorStopped(ctx, pk); err != nil {
								s.logger.Error("validator stopped callback failed", zap.Error(err))
							}
						}(state.share.ValidatorPubKey)
					}
				}
				break
			}
		}
	}

	// Clean up old periods (keep only current and next period)
	currentEpoch := s.beaconCfg.EstimatedCurrentEpoch()
	currentPeriod := s.beaconCfg.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)

	for period := range s.syncCommittees {
		if period < currentPeriod-1 {
			delete(s.syncCommittees, period)
		}
	}

	s.logger.Debug("registered sync committee info",
		zap.Int("info_count", len(info)),
		zap.Int("periods_tracked", len(s.syncCommittees)))

	return nil
}

// GetSyncCommitteeValidators returns validators in the sync committee for a period.
func (s *validatorStoreImpl) GetSyncCommitteeValidators(period uint64) []*ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	periodCommittees, exists := s.syncCommittees[period]
	if !exists {
		return nil
	}

	var snapshots []*ValidatorSnapshot
	processedValidators := make(map[phase0.ValidatorIndex]bool)

	for validatorIndex := range periodCommittees {
		if processedValidators[validatorIndex] {
			continue
		}
		processedValidators[validatorIndex] = true

		pubKey, validatorExists := s.indices[validatorIndex]
		if !validatorExists {
			continue
		}

		key := hex.EncodeToString(pubKey[:])
		state, validatorExists := s.validators[key]
		if !validatorExists {
			continue
		}

		snapshots = append(snapshots, s.createSnapshot(state))
	}

	return snapshots
}

// UpdateValidatorsMetadata updates the metadata for multiple validators.
// It returns only metadata entries that actually changed the stored shares.
// The returned map is nil if no changes occurred.
func (s *validatorStoreImpl) UpdateValidatorsMetadata(ctx context.Context, metadata beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error) {
	if len(metadata) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		changedShares   []*types.SSVShare
		changedMetadata beacon.ValidatorMetadataMap
	)

	for pk, newMetadata := range metadata {
		if newMetadata == nil {
			continue
		}

		key := hex.EncodeToString(pk[:])
		state, exists := s.validators[key]
		if !exists {
			// Validator not found, skip
			continue
		}

		// Check if metadata actually changed
		currentMetadata := state.share.BeaconMetadata()
		if newMetadata.Equals(currentMetadata) {
			continue
		}

		// Update the share with new metadata
		state.share.SetBeaconMetadata(newMetadata)
		state.lastUpdated = time.Now()

		// Recalculate participation status as it may have changed
		oldParticipationStatus := state.participationStatus
		state.participationStatus = s.calculateParticipationStatus(state.share)

		// Collect changed shares for batch save
		changedShares = append(changedShares, state.share)

		// Track changed metadata
		if changedMetadata == nil {
			changedMetadata = make(beacon.ValidatorMetadataMap)
		}
		changedMetadata[pk] = newMetadata

		// Check if participation status changed
		if oldParticipationStatus.IsParticipating != state.participationStatus.IsParticipating {
			snapshot := s.createSnapshot(state)

			if state.participationStatus.IsParticipating && s.shouldStart(state) {
				// Validator became eligible to participate
				if s.callbacks.OnValidatorStarted != nil {
					go func(snap *ValidatorSnapshot) {
						if err := s.callbacks.OnValidatorStarted(ctx, snap); err != nil {
							s.logger.Error("validator started callback failed",
								zap.String("pubkey", hex.EncodeToString(snap.Share.ValidatorPubKey[:])),
								zap.Error(err))
						}
					}(snapshot)
				}
			} else if !state.participationStatus.IsParticipating && oldParticipationStatus.IsParticipating {
				// Validator became ineligible
				if s.callbacks.OnValidatorStopped != nil {
					go func(pk spectypes.ValidatorPK) {
						if err := s.callbacks.OnValidatorStopped(ctx, pk); err != nil {
							s.logger.Error("validator stopped callback failed",
								zap.String("pubkey", hex.EncodeToString(pk[:])),
								zap.Error(err))
						}
					}(state.share.ValidatorPubKey)
				}
			}
		}

		// Always trigger update callback for metadata changes
		if s.callbacks.OnValidatorUpdated != nil {
			snapshot := s.createSnapshot(state)
			go func(snap *ValidatorSnapshot) {
				if err := s.callbacks.OnValidatorUpdated(ctx, snap); err != nil {
					s.logger.Error("validator updated callback failed", zap.Error(err))
				}
			}(snapshot)
		}
	}

	// Persist all changes
	if len(changedShares) > 0 {
		if err := s.sharesStorage.Save(nil, changedShares...); err != nil {
			return nil, fmt.Errorf("persist metadata updates: %w", err)
		}

		s.logger.Debug("metadata updated",
			zap.Int("total_validators", len(metadata)),
			zap.Int("changed_validators", len(changedShares)))
	}

	return changedMetadata, nil
}

// createCommitteeSnapshot creates an immutable snapshot of committee state.
// Requires: caller must hold read lock.
func (s *validatorStoreImpl) createCommitteeSnapshot(committee *committeeState) *CommitteeSnapshot {
	snapshot := &CommitteeSnapshot{
		ID:         committee.id,
		Operators:  make([]spectypes.OperatorID, len(committee.operators)),
		Validators: make([]*ValidatorSnapshot, 0, len(committee.validators)),
	}

	copy(snapshot.Operators, committee.operators)

	// Add validator snapshots
	for pubKey := range committee.validators {
		key := hex.EncodeToString(pubKey[:])
		if state, exists := s.validators[key]; exists {
			snapshot.Validators = append(snapshot.Validators, s.createSnapshot(state))
		}
	}

	return snapshot
}

// isInSyncCommittee checks if a validator is in the sync committee for a period.
// Requires: caller must hold read lock.
func (s *validatorStoreImpl) isInSyncCommittee(validatorIndex phase0.ValidatorIndex, period uint64) bool {
	periodCommittees, exists := s.syncCommittees[period]
	if !exists {
		return false
	}

	_, exists = periodCommittees[validatorIndex]
	return exists
}

// addShareToCommittee adds a share to its corresponding committee structure.
// If the committee does not exist, it is created.
// An OnCommitteeChanged callback is triggered if a new committee is created.
// This method requires the caller to hold the write lock.
func (s *validatorStoreImpl) addShareToCommittee(share *types.SSVShare) error {
	committeeID := share.CommitteeID()

	committee, exists := s.committees[committeeID]
	if !exists {
		// Create new committee
		committee = &committeeState{
			id:         committeeID,
			operators:  share.OperatorIDs(),
			validators: make(map[spectypes.ValidatorPK]struct{}),
		}
		s.committees[committeeID] = committee

		// Schedule callback
		if s.callbacks.OnCommitteeChanged != nil {
			go func() {
				if err := s.callbacks.OnCommitteeChanged(context.Background(), committeeID, CommitteeActionCreated); err != nil {
					s.logger.Error("committee created callback failed",
						zap.String("committee_id", hex.EncodeToString(committeeID[:])),
						zap.Error(err))
				}
			}()
		}
	}

	committee.validators[share.ValidatorPubKey] = struct{}{}
	return nil
}

// updateShareInCommittee ensures a validator's presence in its committee.
// This method is typically called after a share update. Currently, it verifies
// the validator exists within the committee but does not modify committee structure
// beyond what addShareToCommittee or removeShareFromCommittee handle.
// This method requires the caller to hold the write lock.
func (s *validatorStoreImpl) updateShareInCommittee(share *types.SSVShare) error {
	committeeID := share.CommitteeID()

	committee, exists := s.committees[committeeID]
	if !exists {
		return fmt.Errorf("committee not found: %x", committeeID)
	}

	// Validator should already be in the committee, just update reference
	_, exists = committee.validators[share.ValidatorPubKey]
	if !exists {
		return fmt.Errorf("validator not in committee: %x", share.ValidatorPubKey)
	}

	return nil
}

// removeShareFromCommittee removes a share from its committee.
// If the committee becomes empty after the removal, the committee itself is deleted.
// An OnCommitteeChanged callback is triggered if a committee is removed.
// This method requires the caller to hold the write lock.
func (s *validatorStoreImpl) removeShareFromCommittee(share *types.SSVShare) error {
	committeeID := share.CommitteeID()

	committee, exists := s.committees[committeeID]
	if !exists {
		s.logger.Debug("committee not found during share removal",
			zap.String("committee_id", hex.EncodeToString(committeeID[:])),
			zap.String("validator", hex.EncodeToString(share.ValidatorPubKey[:])))
		return nil
	}

	delete(committee.validators, share.ValidatorPubKey)

	// Remove empty committee
	if len(committee.validators) == 0 {
		delete(s.committees, committeeID)

		if s.callbacks.OnCommitteeChanged != nil {
			go func() {
				if err := s.callbacks.OnCommitteeChanged(context.Background(), committeeID, CommitteeActionRemoved); err != nil {
					s.logger.Error("committee removed callback failed",
						zap.String("committee_id", hex.EncodeToString(committeeID[:])),
						zap.Error(err))
				}
			}()
		}
	}

	return nil
}

// copyShare creates a deep copy of the SSVShare object to ensure immutability
// of the shares stored and passed around within the ValidatorStore.
func (s *validatorStoreImpl) copyShare(share *types.SSVShare) *types.SSVShare {
	newShare := &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      share.ValidatorIndex,
			ValidatorPubKey:     share.ValidatorPubKey,
			SharePubKey:         share.SharePubKey,
			DomainType:          share.DomainType,
			FeeRecipientAddress: share.FeeRecipientAddress,
			Graffiti:            share.Graffiti,
		},
		Status:                    share.Status,
		ActivationEpoch:           share.ActivationEpoch,
		ExitEpoch:                 share.ExitEpoch,
		OwnerAddress:              share.OwnerAddress,
		Liquidated:                share.Liquidated,
		BeaconMetadataLastUpdated: share.BeaconMetadataLastUpdated,
	}

	// Deep copy committee
	newShare.Committee = make([]*spectypes.ShareMember, len(share.Committee))
	for i, member := range share.Committee {
		newShare.Committee[i] = &spectypes.ShareMember{
			Signer:      member.Signer,
			SharePubKey: append([]byte(nil), member.SharePubKey...),
		}
	}

	newShare.SetMinParticipationEpoch(share.MinParticipationEpoch())

	return newShare
}

// calculateParticipationStatus determines the current participation status
// of a validator based on its share data, beacon configuration, and estimated current epoch.
// It considers factors like liquidation, beacon metadata, attestation eligibility,
// sync committee membership, and minimum participation epoch.
func (s *validatorStoreImpl) calculateParticipationStatus(share *types.SSVShare) ParticipationStatus {
	status := ParticipationStatus{
		IsLiquidated:      share.Liquidated,
		HasBeaconMetadata: share.HasBeaconMetadata(),
	}

	if status.IsLiquidated {
		status.Reason = "liquidated"
		return status
	}

	if !status.HasBeaconMetadata {
		status.Reason = "missing beacon metadata"
		return status
	}

	epoch := s.beaconCfg.EstimatedCurrentEpoch()

	// Check minimum participation epoch
	status.MinParticipationMet = share.MinParticipationEpoch() <= epoch
	if !status.MinParticipationMet {
		status.Reason = fmt.Sprintf("waiting for min participation epoch %d (current: %d)",
			share.MinParticipationEpoch(), epoch)
		return status
	}

	status.IsAttesting = share.IsAttesting(epoch)
	status.IsSyncCommittee = share.IsSyncCommitteeEligible(s.beaconCfg, epoch)

	status.IsParticipating = share.IsParticipating(s.beaconCfg, epoch)

	if status.IsParticipating {
		if status.IsAttesting {
			status.Reason = "active and attesting"
		} else if status.IsSyncCommittee {
			status.Reason = "sync committee eligible (post-exit)"
		} else {
			status.Reason = "active"
		}
	} else {
		if share.Status.IsExited() || share.Status.HasExited() {
			status.Reason = "exited"
		} else if share.Status == eth2apiv1.ValidatorStatePendingQueued {
			status.Reason = "pending activation"
		} else {
			status.Reason = "not eligible"
		}
	}

	return status
}

// shouldStart determines if a validator, managed by the current operator,
// meets all criteria to be started (i.e., actively participate in duties).
// Criteria include belonging to the operator, being in a participating state,
// and having met its minimum participation epoch.
func (s *validatorStoreImpl) shouldStart(state *validatorState) bool {
	return state.share.BelongsToOperator(s.operatorIDFn()) &&
		state.participationStatus.IsParticipating &&
		state.participationStatus.MinParticipationMet
}

// createSnapshot creates an immutable snapshot of a validator's current state.
// This snapshot includes a deep copy of the share and current participation details,
// making it safe to be used by callbacks and external queries.
func (s *validatorStoreImpl) createSnapshot(state *validatorState) *ValidatorSnapshot {
	return &ValidatorSnapshot{
		Share:               *s.copyShare(state.share),
		LastUpdated:         state.lastUpdated,
		IsOwnValidator:      state.share.BelongsToOperator(s.operatorIDFn()),
		ParticipationStatus: state.participationStatus,
	}
}

// GetSelfValidators returns snapshots of all validators belonging to the current operator.
func (s *validatorStoreImpl) GetSelfValidators() []*ValidatorSnapshot {
	return s.GetOperatorValidators(s.operatorIDFn())
}

// GetSelfParticipatingValidators returns validators belonging to the current operator that are participating.
func (s *validatorStoreImpl) GetSelfParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snapshots []*ValidatorSnapshot
	operatorID := s.operatorIDFn()

	for _, state := range s.validators {
		// Quick check for operator ID
		if !state.share.BelongsToOperator(operatorID) {
			continue
		}

		// Check liquidation filter
		if state.share.Liquidated && !opts.IncludeLiquidated {
			continue
		}

		// Check exit filter
		if state.share.Exited() && !opts.IncludeExited {
			continue
		}

		// Check attesting filter
		if opts.OnlyAttesting && !state.share.IsAttesting(epoch) {
			continue
		}

		// Check sync committee filter
		if opts.OnlySyncCommittee {
			period := s.beaconCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			if !s.isInSyncCommittee(state.share.ValidatorIndex, period) {
				continue
			}
		}

		// Check if participating (unless filtered out above)
		if state.participationStatus.IsParticipating || opts.IncludeLiquidated || opts.IncludeExited {
			snapshots = append(snapshots, s.createSnapshot(state))
		}
	}
	return snapshots
}
