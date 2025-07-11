package metadata

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
)

const (
	testSyncInterval      = 2 * time.Millisecond
	testStreamInterval    = 1 * time.Millisecond
	testUpdateSendTimeout = 3 * time.Millisecond
)

// createTestSnapshot creates a new ValidatorSnapshot with test data based on the provided validator public key, index, and metadata flag.
func createTestSnapshot(pk spectypes.ValidatorPK, index phase0.ValidatorIndex, hasMetadata bool) *storage.ValidatorSnapshot {
	status := eth2apiv1.ValidatorStateUnknown
	if hasMetadata {
		status = eth2apiv1.ValidatorStateActiveOngoing
	}

	return &storage.ValidatorSnapshot{
		Share: ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: pk,
				ValidatorIndex:  index,
			},
			Status:                    status,
			ActivationEpoch:           0,
			ExitEpoch:                 goclient.FarFutureEpoch,
			Liquidated:                false,
			BeaconMetadataLastUpdated: time.Time{}, // Zero for new validators
		},
		LastUpdated:    time.Now(),
		IsOwnValidator: true,
		ParticipationStatus: storage.ParticipationStatus{
			IsParticipating:     hasMetadata,
			IsLiquidated:        false,
			HasBeaconMetadata:   hasMetadata,
			IsAttesting:         hasMetadata,
			IsSyncCommittee:     false,
			MinParticipationMet: true,
			Reason:              "test",
		},
	}
}

// TestUpdateValidatorMetadata tests various scenarios for updating validator metadata in the system, including error handling.
func TestUpdateValidatorMetadata(t *testing.T) {
	passedEpoch := phase0.Epoch(1)

	operatorIDs := []uint64{1, 2, 3, 4}
	committee := make([]*spectypes.ShareMember, len(operatorIDs))
	for i, id := range operatorIDs {
		operatorKey, err := generatePubKey()
		require.NoError(t, err)
		committee[i] = &spectypes.ShareMember{Signer: id, SharePubKey: operatorKey}
	}

	validatorMetadata := &beacon.ValidatorMetadata{Index: 1, ActivationEpoch: passedEpoch, ExitEpoch: goclient.FarFutureEpoch, Status: eth2apiv1.ValidatorStateActiveOngoing}

	pubKey := spectypes.ValidatorPK{0x1}

	testCases := []struct {
		name              string
		metadata          *beacon.ValidatorMetadata
		validatorStoreErr error
		testPublicKey     spectypes.ValidatorPK
	}{
		{"Empty metadata", nil, nil, pubKey},
		{"Valid metadata", validatorMetadata, nil, pubKey},
		{"Share wasn't found", validatorMetadata, nil, pubKey},
		{"Share not belong to operator", validatorMetadata, nil, pubKey},
		{"Metadata with error", validatorMetadata, fmt.Errorf("error"), pubKey},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			logger := logging.TestLogger(t)

			validatorStore := mocks.NewMockValidatorStore(ctrl)
			validatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(nil, tc.validatorStoreErr).AnyTimes()

			data := make(beacon.ValidatorMetadataMap)
			data[tc.testPublicKey] = tc.metadata

			beaconNode := beacon.NewMockBeaconNode(ctrl)
			beaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, pubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
				if tc.metadata == nil {
					return map[phase0.ValidatorIndex]*eth2apiv1.Validator{}, nil
				}

				result := make(map[phase0.ValidatorIndex]*eth2apiv1.Validator)
				for i, pk := range pubKeys {
					result[phase0.ValidatorIndex(i)] = &eth2apiv1.Validator{
						Index:  tc.metadata.Index,
						Status: tc.metadata.Status,
						Validator: &phase0.Validator{
							ActivationEpoch: tc.metadata.ActivationEpoch,
							PublicKey:       pk,
						},
					}
				}
				return result, nil
			}).AnyTimes()

			syncer := NewSyncer(logger, validatorStore, beaconNode, commons.ZeroSubnets)
			_, err := syncer.Sync(context.TODO(), []spectypes.ValidatorPK{tc.testPublicKey})
			if tc.validatorStoreErr != nil {
				require.ErrorIs(t, err, tc.validatorStoreErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSyncer_Sync(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zap.NewNop()

	defaultMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for i, pk := range validatorPubKeys {
			results[phase0.ValidatorIndex(i+1)] = &eth2apiv1.Validator{
				Validator: &phase0.Validator{PublicKey: pk},
			}
		}
		return results, nil
	}).AnyTimes()

	// Subtest: Successful update
	t.Run("Success", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     defaultMockBeaconNode,
		}

		expectedUpdatedShares := beacon.ValidatorMetadataMap{
			spectypes.ValidatorPK{0x1}: {
				Index: 1,
			},
			spectypes.ValidatorPK{0x2}: {
				Index: 2,
			},
		}

		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(expectedUpdatedShares, nil)

		result, err := syncer.Sync(t.Context(), []spectypes.ValidatorPK{{0x1}, {0x2}})
		require.NoError(t, err)
		require.Equal(t, expectedUpdatedShares, result)
	})

	// Subtest: Fetch error
	t.Run("FetchError", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)
		// UpdateValidatorsMetadata should not be called in this case
		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Times(0)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("fetch error"))

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     errMockBeaconNode,
		}

		pubKeys := []spectypes.ValidatorPK{{0x1}, {0x2}}
		result, err := syncer.Sync(t.Context(), pubKeys)
		require.Error(t, err)
		require.Nil(t, result)
	})

	// Subtest: UpdateValidatorsMetadata error
	t.Run("UpdateValidatorsMetadataError", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)
		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("update error"))

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     defaultMockBeaconNode,
		}

		result, err := syncer.Sync(t.Context(), []spectypes.ValidatorPK{{0x1}, {0x2}})
		require.Error(t, err)
		require.Nil(t, result)
	})

	// Subtest: Empty pubKeys
	t.Run("EmptyPubKeys", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)
		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(nil, nil)

		unusedMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		// GetValidatorData should not be called in this case
		unusedMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).Times(0)

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     unusedMockBeaconNode,
		}

		var pubKeys []spectypes.ValidatorPK
		result, err := syncer.Sync(t.Context(), pubKeys)
		require.NoError(t, err)
		require.Nil(t, result)
	})
}

func TestSyncer_UpdateOnStartup(t *testing.T) {
	logger := zap.NewNop()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for i, pk := range validatorPubKeys {
			results[phase0.ValidatorIndex(i+1)] = &eth2apiv1.Validator{
				Validator: &phase0.Validator{PublicKey: pk},
			}
		}
		return results, nil
	}).AnyTimes()

	// Subtest: No validators returned by GetAllValidators
	t.Run("NoShares", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
		}

		// Set expectations - SyncOnStartup calls both GetAllValidators() and GetSelfValidators()
		mockValidatorStore.EXPECT().GetAllValidators().Return([]*storage.ValidatorSnapshot{})
		mockValidatorStore.EXPECT().GetSelfValidators().Return([]*storage.ValidatorSnapshot{}).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(t.Context())

		// Assert
		require.NoError(t, err)
		require.Nil(t, result)
	})

	// Subtest: All shares are non-liquidated and have BeaconMetadata
	t.Run("AllSharesHaveMetadata", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
		}

		// Create snapshots with metadata
		snapshots := []*storage.ValidatorSnapshot{
			createTestSnapshot(spectypes.ValidatorPK{0x1}, 1, true),
			createTestSnapshot(spectypes.ValidatorPK{0x2}, 2, true),
		}

		// Set expectations - SyncOnStartup calls both GetAllValidators() and GetSelfValidators()
		mockValidatorStore.EXPECT().GetAllValidators().Return(snapshots)
		mockValidatorStore.EXPECT().GetSelfValidators().Return(snapshots).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(t.Context())

		// Assert
		require.NoError(t, err)
		require.Nil(t, result)
	})

	// Subtest: At least one validator lacks BeaconMetadata
	t.Run("ShareLacksBeaconMetadata", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     defaultMockBeaconNode,
		}

		// Create snapshots - one with metadata, one without
		snapshot1 := createTestSnapshot(spectypes.ValidatorPK{0x1}, 1, true)
		snapshot2 := createTestSnapshot(spectypes.ValidatorPK{0x2}, 0, false) // No metadata

		snapshots := []*storage.ValidatorSnapshot{snapshot1, snapshot2}

		// Set expectations - SyncOnStartup calls both GetAllValidators() and GetSelfValidators()
		mockValidatorStore.EXPECT().GetAllValidators().Return(snapshots)
		mockValidatorStore.EXPECT().GetSelfValidators().Return(snapshots).AnyTimes()
		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(beacon.ValidatorMetadataMap{
			snapshot1.Share.ValidatorPubKey: snapshot1.Share.BeaconMetadata(),
		}, nil)

		// Call method
		result, err := syncer.SyncOnStartup(t.Context())

		// Assert
		require.NoError(t, err)
		require.Equal(t, beacon.ValidatorMetadataMap{
			snapshot1.Share.ValidatorPubKey: snapshot1.Share.BeaconMetadata(),
		}, result)
	})

	// Subtest: SyncBatch returns error
	t.Run("UpdateError", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
			return nil, fmt.Errorf("error")
		})

		syncer := &Syncer{
			logger:         logger,
			validatorStore: mockValidatorStore,
			beaconNode:     errMockBeaconNode,
		}

		// Create snapshot without metadata
		snapshot1 := createTestSnapshot(spectypes.ValidatorPK{0x1}, 0, false)
		snapshots := []*storage.ValidatorSnapshot{snapshot1}

		// Set expectations - SyncOnStartup calls both GetAllValidators() and GetSelfValidators()
		mockValidatorStore.EXPECT().GetAllValidators().Return(snapshots)
		mockValidatorStore.EXPECT().GetSelfValidators().Return(snapshots).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(t.Context())

		// Assert
		require.Error(t, err)
		require.Nil(t, result)
	})
}

func TestSyncer_Stream(t *testing.T) {
	logger := zap.NewNop()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for i, pk := range validatorPubKeys {
			results[phase0.ValidatorIndex(i+1)] = &eth2apiv1.Validator{
				Validator: &phase0.Validator{PublicKey: pk},
				Index:     1,
				Status:    eth2apiv1.ValidatorStateActiveOngoing,
			}
		}
		return results, nil
	}).AnyTimes()

	// Subtest: Stream sends updates and stops when context is canceled
	t.Run("SendsUpdatesAndStopsOnContextCancel", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			validatorStore:    mockValidatorStore,
			beaconNode:        defaultMockBeaconNode,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Create test snapshot with stale metadata (old timestamp)
		snapshot1 := createTestSnapshot(spectypes.ValidatorPK{0x1}, 1, true)
		// Make the metadata stale by setting an old timestamp
		snapshot1.Share.BeaconMetadataLastUpdated = time.Now().Add(-time.Hour)

		// Use a channel to signal when the update is sent
		updateSent := make(chan struct{})

		// Mock validatorStore methods - Stream calls both GetAllValidators and GetSelfValidators
		mockValidatorStore.EXPECT().GetAllValidators().Return([]*storage.ValidatorSnapshot{snapshot1}).AnyTimes()
		mockValidatorStore.EXPECT().GetSelfValidators().Return([]*storage.ValidatorSnapshot{snapshot1}).AnyTimes()
		mockValidatorStore.EXPECT().UpdateValidatorsMetadata(gomock.Any(), gomock.Any()).Return(beacon.ValidatorMetadataMap{
			snapshot1.Share.ValidatorPubKey: snapshot1.Share.BeaconMetadata(),
		}, nil).AnyTimes()

		// Start Stream
		updates := syncer.Stream(ctx)

		// Read from updates channel using a goroutine
		go func() {
			batch, ok := <-updates
			if !ok {
				t.Error("Updates channel was closed unexpectedly")
				return
			}

			expected := beacon.ValidatorMetadataMap{
				snapshot1.Share.ValidatorPubKey: snapshot1.Share.BeaconMetadata(),
			}

			// Verify the update
			require.Equal(t, expected, batch.Before)
			require.Equal(t, expected, batch.After)
			// Signal that the update was received
			close(updateSent)
		}()

		// Wait for the update to be received or timeout
		select {
		case <-updateSent:
			// SyncBatch received, proceed to cancel the context
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Did not receive update in time")
		}

		// Cancel the context to stop the stream
		cancel()

		// Ensure the updates channel is closed
		_, ok := <-updates
		if ok {
			t.Fatal("Updates channel should be closed after context cancellation")
		}
	})

	// Subtest: Stream handles errors from prepareUpdate
	t.Run("HandlesSendUpdateError", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
			return nil, fmt.Errorf("fetch error")
		}).AnyTimes()

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			validatorStore:    mockValidatorStore,
			beaconNode:        errMockBeaconNode,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Create test snapshot with stale metadata
		snapshot1 := createTestSnapshot(spectypes.ValidatorPK{0x1}, 0, false)
		// Make the metadata stale by setting an old timestamp
		snapshot1.Share.BeaconMetadataLastUpdated = time.Now().Add(-time.Hour)

		// Mock validatorStore methods
		mockValidatorStore.EXPECT().GetAllValidators().Return([]*storage.ValidatorSnapshot{snapshot1}).AnyTimes()
		mockValidatorStore.EXPECT().GetSelfValidators().Return([]*storage.ValidatorSnapshot{snapshot1}).AnyTimes()

		// Start Stream
		updates := syncer.Stream(ctx)

		// Use a channel to signal when the stream has attempted to send an update
		updateAttempted := make(chan struct{})

		// Read from updates channel using a goroutine
		go func() {
			select {
			case update := <-updates:
				t.Errorf("Did not expect an update, but received one: %+v", update)
			case <-time.After(50 * time.Millisecond):
				// No update received, as expected
			}
			close(updateAttempted)
		}()

		// Wait for the update attempt to complete or timeout
		select {
		case <-updateAttempted:
			// SyncBatch attempt completed, proceed to cancel the context
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for update attempt")
		}

		// Cancel the context to stop the stream
		cancel()

		// Ensure the updates channel is closed
		_, ok := <-updates
		if ok {
			t.Fatal("Updates channel should be closed after context cancellation")
		}
	})

	// Subtest: Stream handles empty validators for update
	t.Run("HandlesEmptySharesForUpdate", func(t *testing.T) {
		mockValidatorStore := mocks.NewMockValidatorStore(ctrl)

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			validatorStore:    mockValidatorStore,
			beaconNode:        defaultMockBeaconNode,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Mock validatorStore methods - return empty lists
		mockValidatorStore.EXPECT().GetAllValidators().Return([]*storage.ValidatorSnapshot{}).AnyTimes()
		mockValidatorStore.EXPECT().GetSelfValidators().Return([]*storage.ValidatorSnapshot{}).AnyTimes()

		// Start Stream
		updates := syncer.Stream(ctx)

		// Use a channel to signal when the stream has attempted to send an update
		updateAttempted := make(chan struct{})

		// Read from updates channel using a goroutine
		go func() {
			select {
			case update := <-updates:
				t.Errorf("Did not expect an update, but received one: %+v", update)
			case <-time.After(50 * time.Millisecond):
				// No update received, as expected
			}
			close(updateAttempted)
		}()

		// Wait for the update attempt to complete or timeout
		select {
		case <-updateAttempted:
			// SyncBatch attempt completed, proceed to cancel the context
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for update attempt")
		}

		// Cancel the context to stop the stream
		cancel()

		// Ensure the updates channel is closed
		_, ok := <-updates
		if ok {
			t.Fatal("Updates channel should be closed after context cancellation")
		}
	})
}

func TestWithUpdateInterval(t *testing.T) {
	// Create mock dependencies
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockValidatorStore := mocks.NewMockValidatorStore(ctrl)
	mockBeaconNode := beacon.NewMockBeaconNode(ctrl)

	// Create a logger
	logger := zap.NewNop()

	// Define the interval we want to set
	interval := testSyncInterval * 2

	// Create a Syncer with the WithSyncInterval option
	syncer := NewSyncer(
		logger,
		mockValidatorStore,
		mockBeaconNode,
		commons.ZeroSubnets,
		WithSyncInterval(interval),
	)

	// Check that the syncInterval field is set correctly
	require.Equal(t, interval, syncer.syncInterval, "syncInterval should be set by WithSyncInterval option")
}

func generatePubKey() ([]byte, error) {
	pubKey := make([]byte, 48)
	_, err := rand.Read(pubKey)
	return pubKey, err
}

func TestSyncer_sleep(t *testing.T) {
	// Initialize a no-operation logger to avoid actual logging during tests.
	logger := zap.NewNop()

	// Instantiate the Syncer with the no-op logger.
	syncer := &Syncer{
		logger: logger,
	}

	t.Run("SleptSuccessfully", func(t *testing.T) {
		// Create a background context that won't be canceled.
		ctx := t.Context()

		// Define the sleep duration.
		duration := 50 * time.Millisecond

		// Record the start time.
		start := time.Now()

		// Call the sleep method.
		slept := syncer.sleep(ctx, duration)

		// Calculate the elapsed time.
		elapsed := time.Since(start)

		// Assert that the method returned true.
		require.True(t, slept, "Expected sleep to return true when context is not canceled")

		// Assert that the elapsed time is at least the duration.
		require.GreaterOrEqual(t, elapsed, duration, "Sleep did not last for the expected duration")
	})

	t.Run("ContextCanceledBeforeSleep", func(t *testing.T) {
		// Create a context that is already canceled.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Define the sleep duration.
		duration := 50 * time.Millisecond

		// Record the start time.
		start := time.Now()

		// Call the sleep method.
		slept := syncer.sleep(ctx, duration)

		// Calculate the elapsed time.
		elapsed := time.Since(start)

		// Assert that the method returned false.
		require.False(t, slept, "Expected sleep to return false when context is canceled before sleeping")

		// Assert that the elapsed time is minimal.
		require.Less(t, elapsed, duration, "Sleep should return immediately when context is already canceled")
	})

	t.Run("ContextCanceledDuringSleep", func(t *testing.T) {
		// Create a cancellable context.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Define the sleep duration.
		duration := 100 * time.Millisecond

		// Use a channel to signal when the sleep method returns.
		done := make(chan bool)

		// Start the sleep method in a separate goroutine.
		go func() {
			slept := syncer.sleep(ctx, duration)
			done <- slept
		}()

		// Wait for a shorter duration before canceling the context.
		time.Sleep(50 * time.Millisecond)
		cancel()

		// Wait for the sleep method to return or timeout the test.
		select {
		case slept := <-done:
			// Assert that the method returned false.
			require.False(t, slept, "Expected sleep to return false when context is canceled during sleep")
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Sleep method did not return in expected time after context cancellation")
		}
	})

	t.Run("ZeroDurationSleep", func(t *testing.T) {
		// Create a background context that won't be canceled.
		ctx := t.Context()

		// Define a zero duration.
		duration := 0 * time.Millisecond

		// Record the start time.
		start := time.Now()

		// Call the sleep method.
		slept := syncer.sleep(ctx, duration)

		// Calculate the elapsed time.
		elapsed := time.Since(start)

		// Assert that the method returned true.
		require.True(t, slept, "Expected sleep to return true for zero duration")

		// Assert that the elapsed time is minimal.
		require.Less(t, elapsed, 10*time.Millisecond, "Sleep with zero duration should return immediately")
	})
}
