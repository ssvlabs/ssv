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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	testSyncInterval      = 2 * time.Millisecond
	testStreamInterval    = 1 * time.Millisecond
	testUpdateSendTimeout = 3 * time.Millisecond
)

// This test is copied from validator controller (TestUpdateValidatorMetadata) with minor changes and may require further refactoring.
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
		name             string
		metadata         *beacon.ValidatorMetadata
		sharesStorageErr error
		testPublicKey    spectypes.ValidatorPK
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

			sharesStorage := NewMockshareStorage(ctrl)
			sharesStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(tc.sharesStorageErr).AnyTimes()
			sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			validatorStore := NewMockselfValidatorStore(ctrl)

			data := make(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata)
			data[tc.testPublicKey] = tc.metadata

			beaconNode := beacon.NewMockBeaconNode(ctrl)
			beaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(pubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
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

			noSubnets, err := commons.FromString("0x00000000000000000000000000000000")
			require.NoError(t, err)

			syncer := NewSyncer(logger, sharesStorage, validatorStore, networkconfig.TestNetwork, beaconNode, noSubnets)
			_, err = syncer.Sync(context.TODO(), []spectypes.ValidatorPK{tc.testPublicKey})
			if tc.sharesStorageErr != nil {
				require.ErrorIs(t, err, tc.sharesStorageErr)
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
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
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
		mockShareStorage := NewMockshareStorage(ctrl)
		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(nil)

		syncer := &Syncer{
			logger:       logger,
			shareStorage: mockShareStorage,
			beaconNode:   defaultMockBeaconNode,
		}

		pubKeys := []spectypes.ValidatorPK{{0x1}, {0x2}}
		metadata := ValidatorMap{
			pubKeys[0]: &beacon.ValidatorMetadata{},
			pubKeys[1]: &beacon.ValidatorMetadata{},
		}

		result, err := syncer.Sync(context.Background(), pubKeys)
		require.NoError(t, err)
		require.Equal(t, metadata, result)
	})

	// Subtest: Fetch error
	t.Run("FetchError", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		// UpdateValidatorsMetadata should not be called in this case
		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Times(0)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
			return nil, fmt.Errorf("fetch error")
		})

		syncer := &Syncer{
			logger:       logger,
			shareStorage: mockShareStorage,
			beaconNode:   errMockBeaconNode,
		}

		pubKeys := []spectypes.ValidatorPK{{0x1}, {0x2}}
		result, err := syncer.Sync(context.Background(), pubKeys)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	// Subtest: UpdateValidatorsMetadata error
	t.Run("UpdateValidatorsMetadataError", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(fmt.Errorf("update error"))

		syncer := &Syncer{
			logger:       logger,
			shareStorage: mockShareStorage,
			beaconNode:   defaultMockBeaconNode,
		}

		pubKeys := []spectypes.ValidatorPK{{0x1}, {0x2}}
		metadata := ValidatorMap{
			pubKeys[0]: &beacon.ValidatorMetadata{},
			pubKeys[1]: &beacon.ValidatorMetadata{},
		}

		result, err := syncer.Sync(context.Background(), pubKeys)
		assert.Error(t, err)
		assert.Equal(t, metadata, result)
	})

	// Subtest: Empty pubKeys
	t.Run("EmptyPubKeys", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(nil)

		unusedMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		// GetValidatorData should not be called in this case
		unusedMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).Times(0)

		syncer := &Syncer{
			logger:       logger,
			shareStorage: mockShareStorage,
			beaconNode:   unusedMockBeaconNode,
		}

		pubKeys := []spectypes.ValidatorPK{}
		result, err := syncer.Sync(context.Background(), pubKeys)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestSyncer_UpdateOnStartup(t *testing.T) {
	logger := zap.NewNop()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for i, pk := range validatorPubKeys {
			results[phase0.ValidatorIndex(i+1)] = &eth2apiv1.Validator{
				Validator: &phase0.Validator{PublicKey: pk},
			}
		}
		return results, nil
	}).AnyTimes()

	// Subtest: No shares returned by shareStorage.List
	t.Run("NoShares", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			shareStorage:   mockShareStorage,
			validatorStore: mockValidatorStore,
		}

		// Set expectations
		mockShareStorage.EXPECT().List(nil, gomock.Any()).Return([]*ssvtypes.SSVShare{})
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{}).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(context.Background())

		// Assert
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	// Subtest: All shares are non-liquidated and have BeaconMetadata
	t.Run("AllSharesHaveMetadata", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			shareStorage:   mockShareStorage,
			validatorStore: mockValidatorStore,
		}

		// Create shares that are non-liquidated and have BeaconMetadata
		share1 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x1},
			},
			Status:     eth2apiv1.ValidatorStatePendingInitialized,
			Liquidated: false,
		}
		share2 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x2},
			},
			Status:     eth2apiv1.ValidatorStatePendingInitialized,
			Liquidated: false,
		}

		shares := []*ssvtypes.SSVShare{share1, share2}

		// Set expectations
		mockShareStorage.EXPECT().List(nil, gomock.Any()).Return(shares)
		mockValidatorStore.EXPECT().SelfValidators().Return(shares).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(context.Background())

		// Assert
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	// Subtest: At least one share lacks BeaconMetadata
	t.Run("ShareLacksBeaconMetadata", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		syncer := &Syncer{
			logger:         logger,
			shareStorage:   mockShareStorage,
			validatorStore: mockValidatorStore,
			beaconNode:     defaultMockBeaconNode,
		}

		// Create shares
		share1 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x1},
			},
			Status:     eth2apiv1.ValidatorStatePendingInitialized,
			Liquidated: false,
		}
		share2 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x2},
			},
			Status:     eth2apiv1.ValidatorStateUnknown, // Lacks BeaconMetadata
			Liquidated: false,
		}

		shares := []*ssvtypes.SSVShare{share1, share2}

		// Set expectations
		mockShareStorage.EXPECT().List(nil, gomock.Any()).Return(shares)
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{share1}).AnyTimes()

		// Mock fetcher.Fetch and shareStorage.UpdateValidatorsMetadata
		metadata := ValidatorMap{
			share1.ValidatorPubKey: &beacon.ValidatorMetadata{},
			share2.ValidatorPubKey: &beacon.ValidatorMetadata{},
		}

		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(nil)

		// Call method
		result, err := syncer.SyncOnStartup(context.Background())

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, metadata, result)
	})

	// Subtest: SyncBatch returns error
	t.Run("UpdateError", func(t *testing.T) {
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
			return nil, fmt.Errorf("error")
		})

		syncer := &Syncer{
			logger:         logger,
			shareStorage:   mockShareStorage,
			validatorStore: mockValidatorStore,
			beaconNode:     errMockBeaconNode,
		}

		// Create shares
		share1 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x1},
			},
			Status:     eth2apiv1.ValidatorStateUnknown, // Lacks BeaconMetadata
			Liquidated: false,
		}
		share2 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x2},
			},
			Status:     eth2apiv1.ValidatorStatePendingInitialized,
			Liquidated: false,
		}

		shares := []*ssvtypes.SSVShare{share1, share2}

		// Set expectations
		mockShareStorage.EXPECT().List(nil, gomock.Any()).Return(shares)
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{share1}).AnyTimes()

		// Call method
		result, err := syncer.SyncOnStartup(context.Background())

		// Assert
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestSyncer_Stream(t *testing.T) {
	logger := zap.NewNop()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
	defaultMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
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
		// Mocks
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			shareStorage:      mockShareStorage,
			validatorStore:    mockValidatorStore,
			beaconNode:        defaultMockBeaconNode,
			networkConfig:     networkconfig.TestNetwork,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Share to be returned
		share1 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{0x1},
			},
			Status:          eth2apiv1.ValidatorStateActiveOngoing,
			ActivationEpoch: 0,
			ExitEpoch:       0,
			Liquidated:      false,
		}

		// Use a channel to signal when the update is sent
		updateSent := make(chan struct{})

		// Mock shareStorage.Range
		mockShareStorage.EXPECT().Range(nil, gomock.Any()).DoAndReturn(func(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool) {
			fn(share1)
		}).AnyTimes()

		// Mock shareStorage.UpdateValidatorsMetadata
		mockShareStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(nil).AnyTimes()

		// Mock validatorStore.SelfValidators
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{share1}).AnyTimes()

		// Start Stream
		updates := syncer.Stream(ctx)

		// Read from updates channel using a goroutine
		go func() {
			batch, ok := <-updates
			if !ok {
				t.Error("Updates channel was closed unexpectedly")
				return
			}

			expected := ValidatorMap{
				share1.ValidatorPubKey: &beacon.ValidatorMetadata{
					Index:           1,
					Status:          eth2apiv1.ValidatorStateActiveOngoing,
					ActivationEpoch: 0,
					ExitEpoch:       0,
				},
			}

			// Verify the update
			assert.Equal(t, []phase0.ValidatorIndex{1}, batch.IndicesBefore)
			assert.Equal(t, []phase0.ValidatorIndex{1}, batch.IndicesAfter)
			assert.Equal(t, expected, batch.Validators)
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
		// Mocks
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		errMockBeaconNode := beacon.NewMockBeaconNode(ctrl)
		errMockBeaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
			return nil, fmt.Errorf("fetch error")
		}).AnyTimes()

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			shareStorage:      mockShareStorage,
			validatorStore:    mockValidatorStore,
			beaconNode:        errMockBeaconNode,
			networkConfig:     networkconfig.TestNetwork,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Share to be returned
		share1 := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{0x1},
			},
			Status:     eth2apiv1.ValidatorStatePendingInitialized,
			Liquidated: false,
		}

		// Mock shareStorage.Range
		mockShareStorage.EXPECT().Range(nil, gomock.Any()).DoAndReturn(func(txn basedb.Reader, fn func(*ssvtypes.SSVShare) bool) {
			fn(share1)
		}).AnyTimes()

		// Mock validatorStore.SelfValidators
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{share1}).AnyTimes()

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

	// Subtest: Stream handles empty sharesBatchForUpdate
	t.Run("HandlesEmptySharesForUpdate", func(t *testing.T) {
		// Mocks
		mockShareStorage := NewMockshareStorage(ctrl)
		mockValidatorStore := NewMockselfValidatorStore(ctrl)

		// Syncer instance
		syncer := &Syncer{
			logger:            logger,
			shareStorage:      mockShareStorage,
			validatorStore:    mockValidatorStore,
			beaconNode:        defaultMockBeaconNode,
			networkConfig:     networkconfig.TestNetwork,
			syncInterval:      testSyncInterval,
			streamInterval:    testStreamInterval,
			updateSendTimeout: testUpdateSendTimeout,
		}

		// Context with cancel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Mock shareStorage.Range to not call the callback (no shares)
		mockShareStorage.EXPECT().Range(nil, gomock.Any()).AnyTimes()

		// Mock validatorStore.SelfValidators
		mockValidatorStore.EXPECT().SelfValidators().Return([]*ssvtypes.SSVShare{}).AnyTimes()

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

	mockShareStorage := NewMockshareStorage(ctrl)
	mockValidatorStore := NewMockselfValidatorStore(ctrl)
	mockBeaconNode := beacon.NewMockBeaconNode(ctrl)

	// Create a logger
	logger := zap.NewNop()

	// Define the interval we want to set
	interval := testSyncInterval * 2

	noSubnets, err := commons.FromString("0x00000000000000000000000000000000")
	require.NoError(t, err)

	// Create an Syncer with the WithSyncInterval option
	syncer := NewSyncer(
		logger,
		mockShareStorage,
		mockValidatorStore,
		networkconfig.TestNetwork,
		mockBeaconNode,
		noSubnets,
		WithSyncInterval(interval),
	)

	// Check that the syncInterval field is set correctly
	assert.Equal(t, interval, syncer.syncInterval, "syncInterval should be set by WithSyncInterval option")
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
		ctx := context.Background()

		// Define the sleep duration.
		duration := 50 * time.Millisecond

		// Record the start time.
		start := time.Now()

		// Call the sleep method.
		slept := syncer.sleep(ctx, duration)

		// Calculate the elapsed time.
		elapsed := time.Since(start)

		// Assert that the method returned true.
		assert.True(t, slept, "Expected sleep to return true when context is not canceled")

		// Assert that the elapsed time is at least the duration.
		assert.GreaterOrEqual(t, elapsed, duration, "Sleep did not last for the expected duration")
	})

	t.Run("ContextCanceledBeforeSleep", func(t *testing.T) {
		// Create a context that is already canceled.
		ctx, cancel := context.WithCancel(context.Background())
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
		assert.False(t, slept, "Expected sleep to return false when context is canceled before sleeping")

		// Assert that the elapsed time is minimal.
		assert.Less(t, elapsed, duration, "Sleep should return immediately when context is already canceled")
	})

	t.Run("ContextCanceledDuringSleep", func(t *testing.T) {
		// Create a cancellable context.
		ctx, cancel := context.WithCancel(context.Background())
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
			assert.False(t, slept, "Expected sleep to return false when context is canceled during sleep")
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Sleep method did not return in expected time after context cancellation")
		}
	})

	t.Run("ZeroDurationSleep", func(t *testing.T) {
		// Create a background context that won't be canceled.
		ctx := context.Background()

		// Define a zero duration.
		duration := 0 * time.Millisecond

		// Record the start time.
		start := time.Now()

		// Call the sleep method.
		slept := syncer.sleep(ctx, duration)

		// Calculate the elapsed time.
		elapsed := time.Since(start)

		// Assert that the method returned true.
		assert.True(t, slept, "Expected sleep to return true for zero duration")

		// Assert that the elapsed time is minimal.
		assert.Less(t, elapsed, 10*time.Millisecond, "Sleep with zero duration should return immediately")
	})
}
