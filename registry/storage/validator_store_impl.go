package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// validatorStoreImpl is the concrete implementation of ValidatorIndices.
// It manages all validator state transitions in a thread-safe manner.
type validatorStoreImpl struct {
	logger     *zap.Logger
	db         basedb.Database
	beaconCfg  networkconfig.BeaconConfig
	operatorID spectypes.OperatorID

	mu         sync.RWMutex
	validators map[string]*validatorState
	committees map[spectypes.CommitteeID]*committeeState
	indices    map[phase0.ValidatorIndex]spectypes.ValidatorPK

	callbacks ValidatorLifecycleCallbacks

	// syncCommittees holds the sync committee information. period -> validator -> indices
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
	db basedb.Database,
	beaconCfg networkconfig.BeaconConfig,
	operatorID spectypes.OperatorID,
) ValidatorStore {
	return &validatorStoreImpl{
		logger:         logger.Named("validator_store"),
		db:             db,
		beaconCfg:      beaconCfg,
		operatorID:     operatorID,
		validators:     make(map[string]*validatorState),
		committees:     make(map[spectypes.CommitteeID]*committeeState),
		indices:        make(map[phase0.ValidatorIndex]spectypes.ValidatorPK),
		syncCommittees: make(map[uint64]map[phase0.ValidatorIndex][]phase0.CommitteeIndex),
	}
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
// It creates an immutable copy of the share, updates internal state including
// validator maps, indices, and committee memberships.
// If registered, OnValidatorAdded and OnValidatorStarted (if applicable) callbacks
// are triggered asynchronously.
// Returns an error if the share is nil or if the validator already exists.
func (s *validatorStoreImpl) OnShareAdded(ctx context.Context, share *types.SSVShare) error {
	if share == nil {
		return fmt.Errorf("nil share")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := hex.EncodeToString(share.ValidatorPubKey[:])
	if _, exists := s.validators[key]; exists {
		return fmt.Errorf("validator already exists: %s", key)
	}

	// Create immutable copy
	shareCopy := s.copyShare(share)

	// Create validator state
	state := &validatorState{
		share:               shareCopy,
		lastUpdated:         time.Now(),
		participationStatus: s.calculateParticipationStatus(shareCopy),
	}

	s.validators[key] = state

	// Update indices
	if share.HasBeaconMetadata() {
		s.indices[share.ValidatorIndex] = share.ValidatorPubKey
	}

	if err := s.addShareToCommittee(shareCopy); err != nil {
		return fmt.Errorf("add to committee: %w", err)
	}

	// Create snapshot for callback
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
// It updates the validator's state, including its participation status and beacon metadata.
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

	// Capture new participation state after update
	isParticipating := s.shouldStart(state)
	newSnapshot := s.createSnapshot(state)

	if s.callbacks.OnValidatorUpdated != nil {
		go func() {
			if err := s.callbacks.OnValidatorUpdated(ctx, newSnapshot); err != nil {
				s.logger.Error("validator updated callback failed", zap.Error(err))
			}
		}()
	}

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
// It cleans up the validator's state from internal maps, indices, and committees.
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

	// Remove from indices
	if state.share.HasBeaconMetadata() {
		delete(s.indices, state.share.ValidatorIndex)
	}

	// Remove from committee
	if err := s.removeShareFromCommittee(state.share); err != nil {
		return fmt.Errorf("remove from committee: %w", err)
	}

	// Remove from validators map
	delete(s.validators, key)

	// Call callbacks
	if s.callbacks.OnValidatorRemoved != nil {
		go func() {
			if err := s.callbacks.OnValidatorRemoved(ctx, pubKey); err != nil {
				s.logger.Error("validator removed callback failed", zap.Error(err))
			}
		}()
	}

	// If was participating, also call stop callback
	if s.shouldStart(state) && s.callbacks.OnValidatorStopped != nil {
		go func() {
			if err := s.callbacks.OnValidatorStopped(ctx, pubKey); err != nil {
				s.logger.Error("validator stopped callback failed", zap.Error(err))
			}
		}()
	}

	return nil
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
func (s *validatorStoreImpl) removeShareFromCommittee(share *types.SSVShare) error { //nolint:unused
	committeeID := share.CommitteeID()

	committee, exists := s.committees[committeeID]
	if !exists {
		return fmt.Errorf("committee not found: %x", committeeID)
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

	status.IsAttesting = share.IsAttesting(epoch)
	status.IsSyncCommittee = share.IsSyncCommitteeEligible(s.beaconCfg, epoch)
	status.MinParticipationMet = share.MinParticipationEpoch() <= epoch

	if !status.MinParticipationMet {
		status.Reason = fmt.Sprintf("waiting for min participation epoch %d", share.MinParticipationEpoch())
		return status
	}

	status.IsParticipating = share.IsParticipating(s.beaconCfg, epoch)

	if status.IsParticipating {
		status.Reason = "active"
	} else {
		status.Reason = "not eligible"
	}

	return status
}

// shouldStart determines if a validator, managed by the current operator,
// meets all criteria to be started (i.e., actively participate in duties).
// Criteria include belonging to the operator, being in a participating state,
// and having met its minimum participation epoch.
func (s *validatorStoreImpl) shouldStart(state *validatorState) bool {
	return state.share.BelongsToOperator(s.operatorID) &&
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
		IsOwnValidator:      state.share.BelongsToOperator(s.operatorID),
		ParticipationStatus: state.participationStatus,
	}
}

// OnClusterLiquidated handles the event of a cluster being liquidated.
// It identifies all validators belonging to the specified cluster (by owner and operator IDs),
// marks them as liquidated, updates their participation status, and triggers relevant
// OnValidatorStopped and OnValidatorUpdated callbacks.
func (s *validatorStoreImpl) OnClusterLiquidated(ctx context.Context, owner common.Address, operatorIDs []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute cluster ID to find affected validators
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

func (s *validatorStoreImpl) OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64) error {
	//TODO implement me
	panic("implement me")
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

func (s *validatorStoreImpl) GetValidator(id ValidatorID) (*ValidatorSnapshot, bool) {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetCommittee(id spectypes.CommitteeID) (*CommitteeSnapshot, bool) {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetAllValidators() []*ValidatorSnapshot {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetOperatorValidators(operatorID spectypes.OperatorID) []*ValidatorSnapshot {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetCommittees() []*CommitteeSnapshot {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetOperatorCommittees(operatorID spectypes.OperatorID) []*CommitteeSnapshot {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) RegisterSyncCommitteeInfo(info []SyncCommitteeInfo) error {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) GetSyncCommitteeValidators(period uint64) []*ValidatorSnapshot {
	//TODO implement me
	panic("implement me")
}
