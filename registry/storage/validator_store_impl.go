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

// NewValidatorStore creates a new ValidatorIndices instance.
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

// RegisterLifecycleCallbacks sets the lifecycle callbacks.
func (s *validatorStoreImpl) RegisterLifecycleCallbacks(callbacks ValidatorLifecycleCallbacks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks = callbacks
}

// OnShareAdded handles a new share being added.
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

// addShareToCommittee adds a share to its committee.
// Requires: caller must hold write lock.
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

// updateShareInCommittee updates a share in its committee.
// Requires: caller must hold write lock.
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
// Requires: caller must hold write lock.
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

// copyShare creates a deep copy of the share to ensure immutability.
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

// calculateParticipationStatus determines why a validator is or isn't participating.
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

// shouldStart determines if a validator should be started.
func (s *validatorStoreImpl) shouldStart(state *validatorState) bool {
	return state.share.BelongsToOperator(s.operatorID) &&
		state.participationStatus.IsParticipating &&
		state.participationStatus.MinParticipationMet
}

// createSnapshot creates an immutable snapshot of validator state.
func (s *validatorStoreImpl) createSnapshot(state *validatorState) *ValidatorSnapshot {
	return &ValidatorSnapshot{
		Share:               *s.copyShare(state.share),
		LastUpdated:         state.lastUpdated,
		IsOwnValidator:      state.share.BelongsToOperator(s.operatorID),
		ParticipationStatus: state.participationStatus,
	}
}

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

func (s *validatorStoreImpl) OnClusterReactivated(ctx context.Context, owner common.Address, operatorIDs []uint64) error {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64) error {
	//TODO implement me
	panic("implement me")
}

func (s *validatorStoreImpl) OnOperatorRemoved(ctx context.Context, operatorID spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
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
