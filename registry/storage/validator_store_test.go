package storage

import (
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/ssvlabs/ssv/storage/basedb"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// Test fixtures
var (
	testNetworkConfig = networkconfig.TestNetwork

	testShare1 = &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(1),
			ValidatorPubKey:     spectypes.ValidatorPK{1, 2, 3},
			SharePubKey:         spectypes.ShareValidatorPK{4, 5, 6},
			Committee:           []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}},
			FeeRecipientAddress: [20]byte{10, 20, 30},
			Graffiti:            []byte("test1"),
		},
		Status:          eth2apiv1.ValidatorStateActiveOngoing,
		ActivationEpoch: 100,
		ExitEpoch:       phase0.Epoch(^uint64(0) >> 1), // far future
		OwnerAddress:    common.HexToAddress("0x12345"),
		Liquidated:      false,
	}

	testShare2 = &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(2),
			ValidatorPubKey:     spectypes.ValidatorPK{7, 8, 9},
			SharePubKey:         spectypes.ShareValidatorPK{10, 11, 12},
			Committee:           []*spectypes.ShareMember{{Signer: 2}, {Signer: 3}, {Signer: 4}, {Signer: 5}},
			FeeRecipientAddress: [20]byte{40, 50, 60},
			Graffiti:            []byte("test2"),
		},
		Status:          eth2apiv1.ValidatorStatePendingQueued,
		ActivationEpoch: 200,
		ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
		OwnerAddress:    common.HexToAddress("0x67890"),
		Liquidated:      false,
	}

	testShare3 = &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(3),
			ValidatorPubKey:     spectypes.ValidatorPK{13, 14, 15},
			SharePubKey:         spectypes.ShareValidatorPK{16, 17, 18},
			Committee:           []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}},
			FeeRecipientAddress: [20]byte{70, 80, 90},
			Graffiti:            []byte("test3"),
		},
		Status:          eth2apiv1.ValidatorStateActiveExiting,
		ActivationEpoch: 300,
		ExitEpoch:       400,
		OwnerAddress:    common.HexToAddress("0xabcde"),
		Liquidated:      false,
	}

	// Share without beacon metadata
	testShareNoMetadata = &types.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      0,
			ValidatorPubKey:     spectypes.ValidatorPK{19, 20, 21},
			SharePubKey:         spectypes.ShareValidatorPK{22, 23, 24},
			Committee:           []*spectypes.ShareMember{{Signer: 1}},
			FeeRecipientAddress: [20]byte{100, 110, 120},
			Graffiti:            []byte("no_metadata"),
		},
		Status:       eth2apiv1.ValidatorStateUnknown, // No metadata
		OwnerAddress: common.HexToAddress("0xdeadbeef"),
		Liquidated:   false,
	}
)

type mockSharesStorage struct {
	mu     sync.RWMutex
	shares map[string]*types.SSVShare
}

func newMockSharesStorage() *mockSharesStorage {
	return &mockSharesStorage{
		shares: make(map[string]*types.SSVShare),
	}
}

func (m *mockSharesStorage) Save(txn basedb.ReadWriter, shares ...*types.SSVShare) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, share := range shares {
		key := hex.EncodeToString(share.ValidatorPubKey[:])
		m.shares[key] = share.Copy()
	}
	return nil
}

func (m *mockSharesStorage) Delete(txn basedb.ReadWriter, pubKey []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := hex.EncodeToString(pubKey)
	delete(m.shares, key)
	return nil
}

func (m *mockSharesStorage) List(txn basedb.Reader, filters ...SharesFilter) []*types.SSVShare {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.SSVShare, 0, len(m.shares))
	for _, share := range m.shares {
		result = append(result, share.Copy())
	}
	return result
}

func (m *mockSharesStorage) Get(txn basedb.Reader, pubKey []byte) (*types.SSVShare, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := hex.EncodeToString(pubKey)
	share, exists := m.shares[key]
	if !exists {
		return nil, false
	}
	return share.Copy(), true
}

func (m *mockSharesStorage) Range(txn basedb.Reader, fn func(*types.SSVShare) bool) {
	//TODO implement me
	panic("implement me")
}

func (m *mockSharesStorage) Drop() error {
	//TODO implement me
	panic("implement me")
}

type mockOperatorsStorage struct {
	operators map[spectypes.OperatorID]bool
}

func newMockOperatorsStorage() *mockOperatorsStorage {
	return &mockOperatorsStorage{
		operators: map[spectypes.OperatorID]bool{
			1: true, 2: true, 3: true, 4: true, 5: true,
		},
	}
}

func (m *mockOperatorsStorage) OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error) {
	for _, id := range ids {
		if !m.operators[id] {
			return false, nil
		}
	}
	return true, nil
}

func (m *mockOperatorsStorage) GetOperatorDataByPubKey(r basedb.Reader, operatorPubKey string) (*OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) GetOperatorData(r basedb.Reader, id spectypes.OperatorID) (*OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) SaveOperatorData(rw basedb.ReadWriter, operatorData *OperatorData) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) DeleteOperatorData(rw basedb.ReadWriter, id spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) ListOperators(r basedb.Reader, from uint64, to uint64) ([]OperatorData, error) {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) GetOperatorsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m *mockOperatorsStorage) DropOperators() error {
	//TODO implement me
	panic("implement me")
}

type callbackTracker struct {
	mu               sync.Mutex
	validatorAdded   []spectypes.ValidatorPK
	validatorStarted []spectypes.ValidatorPK
	validatorStopped []spectypes.ValidatorPK
	validatorUpdated []spectypes.ValidatorPK
	validatorRemoved []spectypes.ValidatorPK
	validatorExited  []ExitDescriptor
	committeeChanged []struct {
		ID     spectypes.CommitteeID
		Action CommitteeAction
	}
	indicesChanged int
}

func newCallbackTracker() *callbackTracker {
	return &callbackTracker{}
}

func (ct *callbackTracker) setupCallbacks(store ValidatorStore) {
	store.RegisterLifecycleCallbacks(ValidatorLifecycleCallbacks{
		OnValidatorAdded: func(ctx context.Context, share *ValidatorSnapshot) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorAdded = append(ct.validatorAdded, share.Share.ValidatorPubKey)
			return nil
		},
		OnValidatorStarted: func(ctx context.Context, share *ValidatorSnapshot) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorStarted = append(ct.validatorStarted, share.Share.ValidatorPubKey)
			return nil
		},
		OnValidatorStopped: func(ctx context.Context, pubKey spectypes.ValidatorPK) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorStopped = append(ct.validatorStopped, pubKey)
			return nil
		},
		OnValidatorUpdated: func(ctx context.Context, share *ValidatorSnapshot) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorUpdated = append(ct.validatorUpdated, share.Share.ValidatorPubKey)
			return nil
		},
		OnValidatorRemoved: func(ctx context.Context, pubKey spectypes.ValidatorPK) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorRemoved = append(ct.validatorRemoved, pubKey)
			return nil
		},
		OnValidatorExited: func(ctx context.Context, descriptor ExitDescriptor) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.validatorExited = append(ct.validatorExited, descriptor)
			return nil
		},
		OnCommitteeChanged: func(ctx context.Context, committeeID spectypes.CommitteeID, action CommitteeAction) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.committeeChanged = append(ct.committeeChanged, struct {
				ID     spectypes.CommitteeID
				Action CommitteeAction
			}{ID: committeeID, Action: action})
			return nil
		},
		OnIndicesChanged: func(ctx context.Context) error {
			ct.mu.Lock()
			defer ct.mu.Unlock()
			ct.indicesChanged++
			return nil
		},
	})
}

func createTestStore(t *testing.T, shares ...*types.SSVShare) (ValidatorStore, *mockSharesStorage, *callbackTracker) {
	logger := zaptest.NewLogger(t)
	sharesStorage := newMockSharesStorage()
	operatorsStorage := newMockOperatorsStorage()

	// Pre-populate storage
	for _, share := range shares {
		require.NoError(t, sharesStorage.Save(nil, share))
	}

	store, err := NewValidatorStore(
		logger,
		sharesStorage,
		operatorsStorage,
		testNetworkConfig,
		func() spectypes.OperatorID { return 1 }, // Default operator ID
	)
	require.NoError(t, err)

	tracker := newCallbackTracker()
	tracker.setupCallbacks(store)

	return store, sharesStorage, tracker
}

// Tests

func TestValidatorStore_InitialState(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		require.Empty(t, store.GetAllValidators())
		require.Empty(t, store.GetCommittees())
		require.Empty(t, store.GetSelfValidators())

		// Non-existent lookups
		_, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.False(t, exists)

		_, exists = store.GetValidator(ValidatorIndex(1))
		require.False(t, exists)

		_, exists = store.GetCommittee(testShare1.CommitteeID())
		require.False(t, exists)
	})

	t.Run("with initial shares", func(t *testing.T) {
		store, _, _ := createTestStore(t, testShare1, testShare2)

		validators := store.GetAllValidators()
		require.Len(t, validators, 2)

		committees := store.GetCommittees()
		require.Len(t, committees, 2)

		// Verify share1
		v1, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.Equal(t, testShare1.ValidatorPubKey, v1.Share.ValidatorPubKey)

		// Verify share2
		v2, exists := store.GetValidator(ValidatorIndex(2))
		require.True(t, exists)
		require.Equal(t, testShare2.ValidatorPubKey, v2.Share.ValidatorPubKey)
	})
}

func TestValidatorStore_OnShareAdded(t *testing.T) {
	ctx := t.Context()

	t.Run("add new share with callbacks", func(t *testing.T) {
		store, storage, tracker := createTestStore(t)

		err := store.OnShareAdded(ctx, testShare1, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		// Give callbacks time to execute
		time.Sleep(50 * time.Millisecond)

		// Verify storage
		saved, exists := storage.Get(nil, testShare1.ValidatorPubKey[:])
		require.True(t, exists)
		require.Equal(t, testShare1.ValidatorPubKey, saved.ValidatorPubKey)

		// Verify store state
		v, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.Equal(t, testShare1.ValidatorPubKey, v.Share.ValidatorPubKey)
		require.True(t, v.IsOwnValidator)

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorAdded, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorStarted, testShare1.ValidatorPubKey)
		require.Len(t, tracker.committeeChanged, 1)
		require.Equal(t, CommitteeActionCreated, tracker.committeeChanged[0].Action)
		tracker.mu.Unlock()
	})

	t.Run("add share without callbacks", func(t *testing.T) {
		store, _, tracker := createTestStore(t)

		err := store.OnShareAdded(ctx, testShare2, UpdateOptions{TriggerCallbacks: false})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		tracker.mu.Lock()
		require.Empty(t, tracker.validatorAdded)
		tracker.mu.Unlock()
	})

	t.Run("add nil share", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		err := store.OnShareAdded(ctx, nil, UpdateOptions{})
		require.ErrorContains(t, err, "nil share")
	})

	t.Run("add duplicate share", func(t *testing.T) {
		store, _, _ := createTestStore(t, testShare1)

		err := store.OnShareAdded(ctx, testShare1, UpdateOptions{})
		require.ErrorContains(t, err, "validator already exists")
	})

	t.Run("add share with non-existent operator", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		invalidShare := testShare1.Copy()
		invalidShare.Committee = []*spectypes.ShareMember{{Signer: 999}}

		err := store.OnShareAdded(ctx, invalidShare, UpdateOptions{})
		require.ErrorContains(t, err, "operators don't exist")
	})

	t.Run("add share without metadata", func(t *testing.T) {
		store, _, tracker := createTestStore(t)

		err := store.OnShareAdded(ctx, testShareNoMetadata, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Should be added but not started
		v, exists := store.GetValidator(ValidatorPubKey(testShareNoMetadata.ValidatorPubKey))
		require.True(t, exists)
		require.False(t, v.ParticipationStatus.HasBeaconMetadata)
		require.False(t, v.ParticipationStatus.IsParticipating)

		tracker.mu.Lock()
		require.Contains(t, tracker.validatorAdded, testShareNoMetadata.ValidatorPubKey)
		require.NotContains(t, tracker.validatorStarted, testShareNoMetadata.ValidatorPubKey)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_OnShareUpdated(t *testing.T) {
	ctx := t.Context()

	t.Run("update existing share", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1)

		// Update share
		updatedShare := testShare1.Copy()
		updatedShare.Liquidated = true

		err := store.OnShareUpdated(ctx, updatedShare, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify update
		v, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.True(t, v.Share.Liquidated)
		require.True(t, v.ParticipationStatus.IsLiquidated)

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorUpdated, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorStopped, testShare1.ValidatorPubKey)
		tracker.mu.Unlock()
	})

	t.Run("update non-existent share", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		err := store.OnShareUpdated(ctx, testShare1, UpdateOptions{})
		require.ErrorContains(t, err, "validator not found")
	})

	t.Run("update nil share", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		err := store.OnShareUpdated(ctx, nil, UpdateOptions{})
		require.ErrorContains(t, err, "nil share")
	})

	t.Run("update metadata triggers start", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShareNoMetadata)

		// Update with metadata
		updated := testShareNoMetadata.Copy()
		updated.Status = eth2apiv1.ValidatorStateActiveOngoing
		updated.ActivationEpoch = 0
		updated.ExitEpoch = phase0.Epoch(^uint64(0) >> 1)
		updated.ValidatorIndex = 10

		err := store.OnShareUpdated(ctx, updated, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		v, exists := store.GetValidator(ValidatorIndex(10))
		require.True(t, exists)
		require.True(t, v.ParticipationStatus.HasBeaconMetadata)
		require.True(t, v.ParticipationStatus.IsParticipating)

		tracker.mu.Lock()
		require.Contains(t, tracker.validatorStarted, updated.ValidatorPubKey)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_OnShareRemoved(t *testing.T) {
	ctx := t.Context()

	t.Run("remove existing share", func(t *testing.T) {
		store, storage, tracker := createTestStore(t, testShare1)

		err := store.OnShareRemoved(ctx, testShare1.ValidatorPubKey, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify removal
		_, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.False(t, exists)

		_, exists = storage.Get(nil, testShare1.ValidatorPubKey[:])
		require.False(t, exists)

		// Committee should be removed if empty
		_, exists = store.GetCommittee(testShare1.CommitteeID())
		require.False(t, exists)

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorRemoved, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorStopped, testShare1.ValidatorPubKey)
		require.Len(t, tracker.committeeChanged, 1) // Only Removed (creation happens during initialization)
		require.Equal(t, CommitteeActionRemoved, tracker.committeeChanged[0].Action)
		tracker.mu.Unlock()
	})

	t.Run("remove non-existent share", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		err := store.OnShareRemoved(ctx, testShare1.ValidatorPubKey, UpdateOptions{})
		require.ErrorContains(t, err, "validator not found")
	})
}

func TestValidatorStore_OnClusterLiquidated(t *testing.T) {
	ctx := t.Context()

	t.Run("liquidate cluster", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1, testShare2)

		err := store.OnClusterLiquidated(ctx, testShare1.OwnerAddress, testShare1.OperatorIDs(), UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify liquidation
		v1, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.True(t, v1.Share.Liquidated)
		require.False(t, v1.ParticipationStatus.IsParticipating)

		// Share2 should not be affected
		v2, exists := store.GetValidator(ValidatorPubKey(testShare2.ValidatorPubKey))
		require.True(t, exists)
		require.False(t, v2.Share.Liquidated)

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorStopped, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorUpdated, testShare1.ValidatorPubKey)
		tracker.mu.Unlock()
	})

	t.Run("liquidate non-existent cluster", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		// Should not error
		err := store.OnClusterLiquidated(ctx, common.HexToAddress("0xnone"), []uint64{1, 2, 3, 4}, UpdateOptions{})
		require.NoError(t, err)
	})
}

func TestValidatorStore_OnClusterReactivated(t *testing.T) {
	ctx := t.Context()

	t.Run("reactivate liquidated cluster", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1)

		// First liquidate
		err := store.OnClusterLiquidated(ctx, testShare1.OwnerAddress, testShare1.OperatorIDs(), UpdateOptions{})
		require.NoError(t, err)

		// Then reactivate
		err = store.OnClusterReactivated(ctx, testShare1.OwnerAddress, testShare1.OperatorIDs(), UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify reactivation
		v, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.False(t, v.Share.Liquidated)
		require.True(t, v.ParticipationStatus.IsParticipating)

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorStarted, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorUpdated, testShare1.ValidatorPubKey)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_OnFeeRecipientUpdated(t *testing.T) {
	ctx := t.Context()

	t.Run("update fee recipient", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1, testShare2)

		newRecipient := common.HexToAddress("0xnewrecipient")
		err := store.OnFeeRecipientUpdated(ctx, testShare1.OwnerAddress, newRecipient, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify update
		v1, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.True(t, exists)
		require.Equal(t, newRecipient[:], v1.Share.FeeRecipientAddress[:])

		// Share2 should not be affected
		v2, exists := store.GetValidator(ValidatorPubKey(testShare2.ValidatorPubKey))
		require.True(t, exists)
		require.NotEqual(t, newRecipient, common.Address(v2.Share.FeeRecipientAddress))

		// Verify callbacks
		tracker.mu.Lock()
		require.Contains(t, tracker.validatorUpdated, testShare1.ValidatorPubKey)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_OnValidatorExited(t *testing.T) {
	ctx := t.Context()

	t.Run("validator exit", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1)

		err := store.OnValidatorExited(ctx, testShare1.ValidatorPubKey, 12345, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Verify callback
		tracker.mu.Lock()
		require.Len(t, tracker.validatorExited, 1)
		require.Equal(t, phase0.BLSPubKey(testShare1.ValidatorPubKey), tracker.validatorExited[0].PubKey)
		require.Equal(t, uint64(12345), tracker.validatorExited[0].BlockNumber)
		require.True(t, tracker.validatorExited[0].OwnValidator)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_OnOperatorRemoved(t *testing.T) {
	ctx := t.Context()

	t.Run("remove operator", func(t *testing.T) {
		store, _, tracker := createTestStore(t, testShare1, testShare2)

		// Remove operator 2 (affects testShare1 and testShare2)
		err := store.OnOperatorRemoved(ctx, 2, UpdateOptions{TriggerCallbacks: true})
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Both validators should be removed
		_, exists := store.GetValidator(ValidatorPubKey(testShare1.ValidatorPubKey))
		require.False(t, exists)

		_, exists = store.GetValidator(ValidatorPubKey(testShare2.ValidatorPubKey))
		require.False(t, exists)

		// Verify callbacks
		tracker.mu.Lock()
		require.Len(t, tracker.validatorRemoved, 2)
		require.Contains(t, tracker.validatorRemoved, testShare1.ValidatorPubKey)
		require.Contains(t, tracker.validatorRemoved, testShare2.ValidatorPubKey)
		tracker.mu.Unlock()
	})
}

func TestValidatorStore_GetParticipatingValidators(t *testing.T) {
	store, _, _ := createTestStore(t, testShare1, testShare2, testShare3)

	tests := []struct {
		name     string
		epoch    phase0.Epoch
		opts     ParticipationOptions
		expected int
	}{
		{
			name:     "all participating",
			epoch:    250,
			opts:     ParticipationOptions{},
			expected: 1, // Only testShare1 should be participating
		},
		{
			name:  "include exited",
			epoch: 350,
			opts: ParticipationOptions{
				IncludeExited: true,
			},
			expected: 2, // testShare1 + testShare3
		},
		{
			name:  "only attesting",
			epoch: 250,
			opts: ParticipationOptions{
				OnlyAttesting: true,
			},
			expected: 1, // Only testShare1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validators := store.GetParticipatingValidators(tt.epoch, tt.opts)
			require.Len(t, validators, tt.expected)
		})
	}
}

func TestValidatorStore_UpdateValidatorsMetadata(t *testing.T) {
	ctx := t.Context()
	store, _, _ := createTestStore(t, testShare1, testShareNoMetadata)

	metadata := beacon.ValidatorMetadataMap{
		testShareNoMetadata.ValidatorPubKey: &beacon.ValidatorMetadata{
			ActivationEpoch: 0,
			ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
			Index:           10,
			Status:          eth2apiv1.ValidatorStateActiveOngoing,
		},
		// Update for non-existent validator (should be ignored)
		spectypes.ValidatorPK{99, 99, 99}: &beacon.ValidatorMetadata{
			Index: 999,
		},
	}

	changed, err := store.UpdateValidatorsMetadata(ctx, metadata)
	require.NoError(t, err)
	require.Len(t, changed, 1)
	require.Contains(t, changed, testShareNoMetadata.ValidatorPubKey)

	// Verify metadata updated
	v, exists := store.GetValidator(ValidatorIndex(10))
	require.True(t, exists)
	require.Equal(t, phase0.ValidatorIndex(10), v.Share.ValidatorIndex)
	require.True(t, v.ParticipationStatus.HasBeaconMetadata)

	// Update with same metadata (no change)
	changed, err = store.UpdateValidatorsMetadata(ctx, metadata)
	require.NoError(t, err)
	require.Empty(t, changed)
}

func TestValidatorStore_Concurrency(t *testing.T) {
	ctx := t.Context()
	store, _, _ := createTestStore(t)

	// Create many shares
	shares := make([]*types.SSVShare, 100)
	for i := range shares {
		shares[i] = &types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  phase0.ValidatorIndex(i + 1),
				ValidatorPubKey: spectypes.ValidatorPK{byte(i), byte(i + 1), byte(i + 2)},
				SharePubKey:     spectypes.ShareValidatorPK{byte(i + 3), byte(i + 4), byte(i + 5)},
				Committee:       []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}},
			},
		}
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent adds
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := store.OnShareAdded(ctx, shares[idx], UpdateOptions{}); err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.GetAllValidators()
			_ = store.GetCommittees()
			_ = store.GetParticipatingValidators(0, ParticipationOptions{})
		}()
	}

	// Concurrent updates
	for i := 50; i < 70; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// First add
			if err := store.OnShareAdded(ctx, shares[idx], UpdateOptions{}); err != nil {
				errors <- err
				return
			}
			// Then update
			updated := shares[idx].Copy()
			updated.Liquidated = true
			if err := store.OnShareUpdated(ctx, updated, UpdateOptions{}); err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent removes
	for i := 70; i < 80; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// First add
			if err := store.OnShareAdded(ctx, shares[idx], UpdateOptions{}); err != nil {
				errors <- err
				return
			}
			// Then remove
			if err := store.OnShareRemoved(ctx, shares[idx].ValidatorPubKey, UpdateOptions{}); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		require.NoError(t, err)
	}

	// Verify final state consistency
	validators := store.GetAllValidators()
	require.GreaterOrEqual(t, len(validators), 50) // At least the first 50 + some updates
}

func TestValidatorStore_GetSelfOperators(t *testing.T) {
	// Create store with operator ID 2
	logger := zaptest.NewLogger(t)
	sharesStorage := newMockSharesStorage()
	operatorsStorage := newMockOperatorsStorage()

	require.NoError(t, sharesStorage.Save(nil, testShare1, testShare2))

	store, err := NewValidatorStore(
		logger,
		sharesStorage,
		operatorsStorage,
		testNetworkConfig,
		func() spectypes.OperatorID { return 2 },
	)
	require.NoError(t, err)

	// Get self validators (operator 2)
	selfValidators := store.GetSelfValidators()
	require.Len(t, selfValidators, 2) // Both shares have operator 2

	// Get self participating validators
	selfParticipating := store.GetSelfParticipatingValidators(250, ParticipationOptions{})
	require.Len(t, selfParticipating, 1) // Only testShare1 is participating at epoch 250 (testShare2 is pending)
}

func TestValidatorStore_CommitteeManagement(t *testing.T) {
	ctx := t.Context()
	store, _, tracker := createTestStore(t)

	// Add two validators to same committee
	share1 := testShare1.Copy()
	share2 := testShare1.Copy()
	share2.ValidatorPubKey = spectypes.ValidatorPK{25, 26, 27}
	share2.ValidatorIndex = 10

	err := store.OnShareAdded(ctx, share1, UpdateOptions{TriggerCallbacks: true})
	require.NoError(t, err)

	err = store.OnShareAdded(ctx, share2, UpdateOptions{TriggerCallbacks: true})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Check committee
	committee, exists := store.GetCommittee(share1.CommitteeID())
	require.True(t, exists)
	require.Len(t, committee.Validators, 2)
	require.Equal(t, share1.OperatorIDs(), committee.Operators)

	// Remove one validator
	err = store.OnShareRemoved(ctx, share1.ValidatorPubKey, UpdateOptions{TriggerCallbacks: true})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Committee should still exist with one validator
	committee, exists = store.GetCommittee(share1.CommitteeID())
	require.True(t, exists)
	require.Len(t, committee.Validators, 1)

	// Remove last validator
	err = store.OnShareRemoved(ctx, share2.ValidatorPubKey, UpdateOptions{TriggerCallbacks: true})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Committee should be removed
	_, exists = store.GetCommittee(share1.CommitteeID())
	require.False(t, exists)

	// Verify callbacks
	tracker.mu.Lock()
	require.Len(t, tracker.committeeChanged, 2) // Created once, removed once
	tracker.mu.Unlock()
}

func TestValidatorStore_ParticipationStatus(t *testing.T) {
	tests := []struct {
		name     string
		share    *types.SSVShare
		expected ParticipationStatus
	}{
		{
			name:  "active validator",
			share: testShare1,
			expected: ParticipationStatus{
				IsParticipating:     true,
				HasBeaconMetadata:   true,
				IsAttesting:         true,
				MinParticipationMet: true,
				Reason:              "active and attesting",
			},
		},
		{
			name:  "pending validator",
			share: testShare2,
			expected: ParticipationStatus{
				IsParticipating:     false,
				HasBeaconMetadata:   true,
				IsAttesting:         true,
				MinParticipationMet: true,
				Reason:              "pending activation",
			},
		},
		{
			name:  "exiting validator",
			share: testShare3,
			expected: ParticipationStatus{
				IsParticipating:     true,
				HasBeaconMetadata:   true,
				IsAttesting:         true,
				MinParticipationMet: true,
				Reason:              "active and attesting",
			},
		},
		{
			name:  "no metadata",
			share: testShareNoMetadata,
			expected: ParticipationStatus{
				IsParticipating:   false,
				HasBeaconMetadata: false,
				Reason:            "missing beacon metadata",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _, _ := createTestStore(t, tt.share)

			v, exists := store.GetValidator(ValidatorPubKey(tt.share.ValidatorPubKey))
			require.True(t, exists)

			require.Equal(t, tt.expected.IsParticipating, v.ParticipationStatus.IsParticipating)
			require.Equal(t, tt.expected.HasBeaconMetadata, v.ParticipationStatus.HasBeaconMetadata)
			require.Equal(t, tt.expected.IsAttesting, v.ParticipationStatus.IsAttesting)
			require.Equal(t, tt.expected.Reason, v.ParticipationStatus.Reason)
		})
	}
}

func TestValidatorStore_GetValidatorStatusReport(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		store, _, _ := createTestStore(t)

		report := store.GetValidatorStatusReport()
		require.Empty(t, report)
	})

	t.Run("single validator statuses", func(t *testing.T) {
		// Create shares that belong to operator 1
		activeShare := testShare1.Copy() // ActiveOngoing, belongs to operator 1

		pendingShare := testShare2.Copy()                                                                     // PendingQueued
		pendingShare.Committee = []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}} // Make it belong to operator 1

		exitingShare := testShare3.Copy() // ActiveExiting, belongs to operator 1

		noMetadataShare := testShareNoMetadata.Copy() // No metadata, belongs to operator 1

		tests := []struct {
			name             string
			share            *types.SSVShare
			expectedStatuses map[ValidatorStatus]uint32
		}{
			{
				name:  "active participating validator",
				share: activeShare, // IsParticipating: true, IsActive: true
				expectedStatuses: map[ValidatorStatus]uint32{
					ValidatorStatusParticipating: 1,
					ValidatorStatusActive:        1,
				},
			},
			{
				name:  "pending validator",
				share: pendingShare, // IsParticipating: true, Pending: true, Activated: false
				expectedStatuses: map[ValidatorStatus]uint32{
					ValidatorStatusParticipating: 1,
					ValidatorStatusNotActivated:  1, // Because Activated() returns false
				},
			},
			{
				name:  "exiting validator",
				share: exitingShare, // IsParticipating: true, IsActive: false, Exited: false
				expectedStatuses: map[ValidatorStatus]uint32{
					ValidatorStatusParticipating: 1,
					ValidatorStatusUnknown:       1, // Falls through to unknown since it's not active/slashed/exited/etc
				},
			},
			{
				name:  "validator without metadata",
				share: noMetadataShare, // HasBeaconMetadata: false
				expectedStatuses: map[ValidatorStatus]uint32{
					ValidatorStatusNotFound: 1,
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				store, _, _ := createTestStore(t, tt.share)

				report := store.GetValidatorStatusReport()
				t.Logf("Actual report for %s: %+v", tt.name, report)

				// Verify all expected statuses are present with correct counts
				for expectedStatus, expectedCount := range tt.expectedStatuses {
					actualCount, exists := report[expectedStatus]
					require.True(t, exists)
					require.Equal(t, expectedCount, actualCount)
				}

				// Verify no other statuses have counts (except the expected ones)
				for status, count := range report {
					if count > 0 {
						_, expected := tt.expectedStatuses[status]
						require.True(t, expected)
					}
				}
			})
		}
	})

	t.Run("multiple validators mixed statuses", func(t *testing.T) {
		activeShare := testShare1.Copy() // ActiveOngoing

		pendingShare := testShare2.Copy() // PendingQueued
		pendingShare.Committee = []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}}

		exitingShare := testShare3.Copy() // ActiveExiting

		noMetadataShare := testShareNoMetadata.Copy() // Status=Unknown, no metadata

		liquidatedShare := testShare1.Copy()
		liquidatedShare.ValidatorPubKey = spectypes.ValidatorPK{50, 51, 52}
		liquidatedShare.ValidatorIndex = 50
		liquidatedShare.Liquidated = true

		slashedShare := testShare1.Copy()
		slashedShare.ValidatorPubKey = spectypes.ValidatorPK{60, 61, 62}
		slashedShare.ValidatorIndex = 60
		slashedShare.Status = eth2apiv1.ValidatorStateActiveSlashed

		noIndexShare := &types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  0,
				ValidatorPubKey: spectypes.ValidatorPK{70, 71, 72},
				SharePubKey:     spectypes.ShareValidatorPK{73, 74, 75},
				Committee:       []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}},
			},
			Status:          eth2apiv1.ValidatorStateUnknown, // No metadata = Unknown status
			ActivationEpoch: 0,
			ExitEpoch:       0,
			OwnerAddress:    common.HexToAddress("0x12345"),
			Liquidated:      false,
		}

		store, _, _ := createTestStore(t,
			activeShare,
			pendingShare,
			exitingShare,
			noMetadataShare,
			liquidatedShare,
			slashedShare,
			noIndexShare,
		)

		report := store.GetValidatorStatusReport()

		expected := map[ValidatorStatus]uint32{
			ValidatorStatusParticipating: 3, // activeShare, exitingShare, slashedShare
			ValidatorStatusActive:        2, // activeShare + liquidatedShare
			ValidatorStatusNotActivated:  1, // pendingShare
			ValidatorStatusUnknown:       1, // exitingShare (ActiveExiting falls through)
			ValidatorStatusNotFound:      2, // noMetadataShare + noIndexShare
			ValidatorStatusSlashed:       1, // slashedShare
		}

		for status, expectedCount := range expected {
			actualCount := report[status]
			require.Equal(t, expectedCount, actualCount,
				"Status %v: expected %d, got %d", status, expectedCount, actualCount)
		}
	})

	t.Run("only self validators included", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		sharesStorage := newMockSharesStorage()
		operatorsStorage := newMockOperatorsStorage()

		// Create shares belonging to different operators
		ownShare := testShare1.Copy() // Has operator 1 in committee
		ownShare.ValidatorPubKey = spectypes.ValidatorPK{80, 81, 82}

		otherShare := testShare1.Copy() // Replace committee to not include operator 1
		otherShare.ValidatorPubKey = spectypes.ValidatorPK{90, 91, 92}
		otherShare.Committee = []*spectypes.ShareMember{{Signer: 5}, {Signer: 6}, {Signer: 7}, {Signer: 8}}

		require.NoError(t, sharesStorage.Save(nil, ownShare, otherShare))

		store, err := NewValidatorStore(
			logger,
			sharesStorage,
			operatorsStorage,
			testNetworkConfig,
			func() spectypes.OperatorID { return 1 }, // Only operator 1
		)
		require.NoError(t, err)

		report := store.GetValidatorStatusReport()

		// Should only count validators belonging to operator 1
		selfValidators := store.GetSelfValidators()
		require.Len(t, selfValidators, 1)

		totalCount := uint32(0)
		for _, count := range report {
			totalCount += count
		}

		// Since one validator can have multiple statuses, the total count can be > number of validators
		require.Greater(t, totalCount, uint32(0))

		// Should be the own share status (participating + active)
		require.Greater(t, report[ValidatorStatusParticipating], uint32(0))
		require.Greater(t, report[ValidatorStatusActive], uint32(0))
	})

	t.Run("status report consistency", func(t *testing.T) {
		pendingShare := testShare2.Copy()
		pendingShare.Committee = []*spectypes.ShareMember{{Signer: 1}, {Signer: 2}, {Signer: 3}, {Signer: 4}}

		store, _, _ := createTestStore(t, testShare1, pendingShare, testShare3, testShareNoMetadata)

		// Get report multiple times to ensure consistency
		report1 := store.GetValidatorStatusReport()
		report2 := store.GetValidatorStatusReport()
		report3 := store.GetValidatorStatusReport()

		require.Equal(t, report1, report2)
		require.Equal(t, report2, report3)

		// Verify that all self-validators are counted (though they may appear in multiple categories)
		selfValidators := store.GetSelfValidators()
		require.Len(t, selfValidators, 4) // All test shares belong to operator 1

		// Each validator should appear in at least one status category
		require.Greater(t, len(report1), 0)

		totalUniqueStatuses := 0
		for _, count := range report1 {
			if count > 0 {
				totalUniqueStatuses++
			}
		}
		require.Greater(t, totalUniqueStatuses, 0)
	})
}
