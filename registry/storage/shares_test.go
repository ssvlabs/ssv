package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/threshold"
)

func init() {
	threshold.Init()
}

func TestValidatorSerializer(t *testing.T) {
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	const keysCount = 13

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	validatorShare := generateRandomValidatorStorageShare(splitKeys)
	b, err := validatorShare.Encode()
	require.NoError(t, err)

	obj := basedb.Obj{Value: b}
	v1 := &Share{}
	require.NoError(t, v1.Decode(obj.Value))
	require.NotNil(t, v1.ValidatorPubKey)
	require.Equal(t, hex.EncodeToString(v1.ValidatorPubKey[:]), hex.EncodeToString(validatorShare.ValidatorPubKey[:]))
	require.NotNil(t, v1.Committee)
	require.Equal(t, v1.ValidatorIndex, validatorShare.ValidatorIndex)
	require.Equal(t, v1.Status, validatorShare.Status)
	require.Equal(t, v1.ActivationEpoch, validatorShare.ActivationEpoch)
	require.Equal(t, v1.ExitEpoch, validatorShare.ExitEpoch)
	require.Equal(t, v1.OwnerAddress, validatorShare.OwnerAddress)
	require.Equal(t, v1.Liquidated, validatorShare.Liquidated)

	tooBigEncodedShare := bytes.Repeat(obj.Value, 20)
	require.ErrorContains(t, v1.Decode(tooBigEncodedShare),
		"share size is too big, got "+strconv.Itoa(len(tooBigEncodedShare))+", max allowed "+strconv.Itoa(ssvtypes.MaxAllowedShareSize))
}

func TestMaxPossibleShareSize(t *testing.T) {
	s, err := generateMaxPossibleShare()
	require.NoError(t, err)

	b, err := s.Encode()
	require.NoError(t, err)

	require.LessOrEqual(t, len(b), ssvtypes.MaxPossibleShareSize)
}

func TestSharesStorage(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	for operatorID := range splitKeys {
		_, err = storage.Operators.SaveOperatorData(nil, &OperatorData{ID: operatorID, PublicKey: strconv.FormatUint(operatorID, 10)})
		require.NoError(t, err)
	}

	var persistedActiveValidatorShares []*ssvtypes.SSVShare
	persistedActiveValidatorShares = append(persistedActiveValidatorShares,
		generateRandomShare(splitKeys, v1.ValidatorStateActiveOngoing, true),
		generateRandomShare(splitKeys, v1.ValidatorStateActiveOngoing, false))

	for _, share := range persistedActiveValidatorShares {
		require.NoError(t, storage.Shares.Save(nil, share))
	}

	t.Run("Get_sharesExist", func(t *testing.T) {
		for _, share := range persistedActiveValidatorShares {
			fetchedShare, exists := storage.Shares.Get(nil, share.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, fetchedShare)
			require.EqualValues(t, hex.EncodeToString(share.ValidatorPubKey[:]), hex.EncodeToString(fetchedShare.ValidatorPubKey[:]))
			require.EqualValues(t, share.Committee, fetchedShare.Committee)
			require.Equal(t, share.Status, fetchedShare.Status)
			require.Equal(t, share.ActivationEpoch, fetchedShare.ActivationEpoch)
			require.Equal(t, share.ExitEpoch, fetchedShare.ExitEpoch)
			require.Equal(t, share.OwnerAddress, fetchedShare.OwnerAddress)
			require.Equal(t, share.Liquidated, fetchedShare.Liquidated)
			require.Equal(t, share.FeeRecipientAddress, fetchedShare.FeeRecipientAddress)
			require.Equal(t, share.Graffiti, fetchedShare.Graffiti)
			require.Equal(t, share.DomainType, fetchedShare.DomainType)
			require.Equal(t, share.SharePubKey, fetchedShare.SharePubKey)
			require.Equal(t, share.ValidatorIndex, fetchedShare.ValidatorIndex)
			require.Equal(t, share.BeaconMetadataLastUpdated, fetchedShare.BeaconMetadataLastUpdated)
		}
	})

	t.Run("UpdateValidatorMetadata_updatesMetadata_whenMetadataChanged", func(t *testing.T) {
		for _, share := range persistedActiveValidatorShares {
			updatedIndex := phase0.ValidatorIndex(rand.Uint64())
			updatedActivationEpoch, updatedExitEpoch := phase0.Epoch(5), phase0.Epoch(6)
			updatedStatus := v1.ValidatorStateActiveOngoing

			updatedShares, err := storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
				share.ValidatorPubKey: {
					Index:           updatedIndex,
					Status:          updatedStatus,
					ActivationEpoch: updatedActivationEpoch,
					ExitEpoch:       updatedExitEpoch,
				}})
			require.NoError(t, err)
			require.NotNil(t, updatedShares)

			fetchedShare, exists := storage.Shares.Get(nil, share.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, fetchedShare)
			require.Equal(t, updatedIndex, fetchedShare.ValidatorIndex)
			require.Equal(t, updatedActivationEpoch, fetchedShare.ActivationEpoch)
			require.Equal(t, updatedExitEpoch, fetchedShare.ExitEpoch)
			require.Equal(t, updatedStatus, fetchedShare.Status)
			require.WithinDuration(t, time.Now(), fetchedShare.BeaconMetadataLastUpdated, time.Second)
		}
	})

	t.Run("UpdateValidatorMetadata_updatesMetadata_whenMetadataUnchanged", func(t *testing.T) {
		for _, share := range persistedActiveValidatorShares {
			updatedShares, err := storage.Shares.UpdateValidatorsMetadata(
				map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{share.ValidatorPubKey: share.BeaconMetadata()})
			require.NoError(t, err)
			require.Nil(t, updatedShares)

			fetchedShare, exists := storage.Shares.Get(nil, share.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, fetchedShare)
			require.WithinDuration(t, time.Now(), fetchedShare.BeaconMetadataLastUpdated, time.Second)
		}
	})

	t.Run("List_NoFilter", func(t *testing.T) {
		shares := storage.Shares.List(nil)

		require.NoError(t, err)
		require.EqualValues(t, len(persistedActiveValidatorShares), len(shares))
		for _, share := range shares {
			require.NotNil(t, share)
			require.Contains(t, persistedActiveValidatorShares, share)
		}
	})

	t.Run("List_Filter_ByClusterId", func(t *testing.T) {
		for _, share := range persistedActiveValidatorShares {
			clusterID := ssvtypes.ComputeClusterIDHash(share.OwnerAddress, []uint64{1, 2, 3, 4})

			validators := storage.Shares.List(nil, ByClusterIDHash(clusterID))
			require.Equal(t, 2, len(validators))
		}
	})

	t.Run("List_Filter_ByOperatorID", func(t *testing.T) {
		validators := storage.Shares.List(nil, ByOperatorID(1))
		require.Equal(t, 2, len(validators))
	})

	t.Run("List_Filter_ByActiveValidator", func(t *testing.T) {
		validators := storage.Shares.List(nil, ByActiveValidator())
		require.Equal(t, 2, len(validators))
	})

	t.Run("List_Filter_ByNotLiquidated", func(t *testing.T) {
		validators := storage.Shares.List(nil, ByNotLiquidated())
		require.Equal(t, 1, len(validators))
	})

	t.Run("KV_reuse_works", func(t *testing.T) {
		storageDuplicate, _, err := NewSharesStorage(networkconfig.TestNetwork.Beacon, storage.db, &noOpRecipientReader{}, []byte("test"))
		require.NoError(t, err)
		existingValidators := storageDuplicate.List(nil)

		require.Equal(t, 2, len(existingValidators))
	})
}

func TestValidatorPubkeysToIndicesMapping(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	for operatorID := range splitKeys {
		_, err = storage.Operators.SaveOperatorData(nil, &OperatorData{ID: operatorID, PublicKey: strconv.FormatUint(operatorID, 10)})
		require.NoError(t, err)
	}

	validatorShare := generateRandomShare(splitKeys, v1.ValidatorStateActiveOngoing, false)
	validatorShare.ValidatorIndex = 3
	validatorShare.ActivationEpoch = 4
	validatorShare.OwnerAddress = common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e")
	validatorShare.Liquidated = false
	require.NoError(t, storage.Shares.Save(nil, validatorShare))

	validatorShare2 := generateRandomShare(splitKeys, v1.ValidatorStateActiveOngoing, false)
	validatorShare2.ValidatorIndex = 55
	require.NoError(t, storage.Shares.Save(nil, validatorShare2))

	m, err := storage.Shares.(*sharesStorage).loadPubkeyToIndexMappings()
	require.NoError(t, err)
	require.Equal(t, 2, len(m))
	require.Equal(t, validatorShare.ValidatorIndex, m[validatorShare.ValidatorPubKey])
	require.Equal(t, validatorShare2.ValidatorIndex, m[validatorShare2.ValidatorPubKey])

	pubkeys := []spectypes.ValidatorPK{validatorShare.ValidatorPubKey, validatorShare2.ValidatorPubKey}
	indices, err := storage.Shares.(*sharesStorage).GetValidatorIndicesByPubkeys(pubkeys)
	require.NoError(t, err)
	require.Equal(t, 2, len(indices))
	// should maintain order
	assert.Equal(t, validatorShare.ValidatorIndex, indices[0])
	assert.Equal(t, validatorShare2.ValidatorIndex, indices[1])
}

func TestShareDeletionHandlesValidatorStoreCorrectly(t *testing.T) {
	logger := log.TestLogger(t)

	// Test share deletion with and without reopening the database.
	testWithStorageReopen(t, func(t *testing.T, storage *testStorage, reopen func(t *testing.T)) {
		// Generate and save a random validator share
		validatorShare := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
		require.NoError(t, storage.Shares.Save(nil, validatorShare))
		reopen(t)

		// Ensure the share is saved correctly
		savedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
		require.True(t, exists)
		require.NotNil(t, savedShare)

		// Ensure the share is saved correctly in the validatorStore
		validatorShareFromStore, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
		require.True(t, exists)
		require.NotNil(t, validatorShareFromStore)

		// Verify that other internal mappings are updated accordingly
		requireValidatorStoreIntegrity(t, storage.ValidatorStore, []*ssvtypes.SSVShare{validatorShare})

		// Delete the share from storage
		require.NoError(t, storage.Shares.Delete(nil, validatorShare.ValidatorPubKey[:]))
		reopen(t)

		// Verify that the share is deleted from shareStorage
		deletedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
		require.False(t, exists)
		require.Nil(t, deletedShare, "Share should be deleted from shareStorage")

		// Verify that the validatorStore reflects the removal correctly
		removedShare, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
		require.False(t, exists)
		require.Nil(t, removedShare, "Share should be removed from validator store after deletion")

		// Further checks on internal data structures
		committee, exists := storage.ValidatorStore.Committee(validatorShare.CommitteeID())
		require.False(t, exists)
		require.Nil(t, committee, "Committee should be nil after share deletion")

		// Verify that other internal mappings are updated accordingly
		byIndex, exists := storage.ValidatorStore.ValidatorByIndex(validatorShare.ValidatorIndex)
		require.False(t, exists)
		require.Nil(t, byIndex)
		for _, operator := range validatorShare.Committee {
			shares := storage.ValidatorStore.OperatorValidators(operator.Signer)
			require.Empty(t, shares, "Data for operator should be nil after share deletion")
		}
		require.Empty(t, storage.ValidatorStore.OperatorValidators(100))
		require.Empty(t, storage.ValidatorStore.Committees())

		// Cleanup the share storage for the next test
		require.NoError(t, storage.Shares.Drop())
		reopen(t)
		validators := storage.Shares.List(nil)
		require.EqualValues(t, 0, len(validators), "No validators should be left in storage after drop")
		requireValidatorStoreIntegrity(t, storage.ValidatorStore, []*ssvtypes.SSVShare{})
	})

	t.Run("share_gone_after_db_recreation", func(t *testing.T) {
		storage, err := newTestStorage(logger)
		require.NoError(t, err)
		defer storage.Close()

		validatorShare := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
		require.NoError(t, storage.Shares.Save(nil, validatorShare))

		requireValidatorStoreIntegrity(t, storage.ValidatorStore, []*ssvtypes.SSVShare{validatorShare})

		require.NoError(t, storage.Recreate(logger))

		requireValidatorStoreIntegrity(t, storage.ValidatorStore, []*ssvtypes.SSVShare{})
	})
}

func TestValidatorStoreThroughSharesStorage(t *testing.T) {
	testWithStorageReopen(t, func(t *testing.T, storage *testStorage, reopen func(t *testing.T)) {
		// Generate and save a random validator share
		testShare := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
		require.NoError(t, storage.Shares.Save(nil, testShare))
		reopen(t)

		// Try saving nil share/shares
		require.Error(t, storage.Shares.Save(nil, nil))
		require.Error(t, storage.Shares.Save(nil, nil, testShare))
		require.Error(t, storage.Shares.Save(nil, testShare, nil))

		// Ensure the share is saved correctly
		savedShare, exists := storage.Shares.Get(nil, testShare.ValidatorPubKey[:])
		require.True(t, exists)
		require.NotNil(t, savedShare)

		// Verify that the validatorStore has the share via SharesStorage
		storedShare, exists := storage.ValidatorStore.Validator(testShare.ValidatorPubKey[:])
		require.True(t, exists)
		require.NotNil(t, storedShare, "Share should be present in validator store after adding to sharesStorage")
		requireValidatorStoreIntegrity(t, storage.ValidatorStore, []*ssvtypes.SSVShare{testShare})

		// Now update the share
		updatedMetadata := &beaconprotocol.ValidatorMetadata{
			Status:          v1.ValidatorStateActiveExiting,
			Index:           1,
			ActivationEpoch: 4,
			ExitEpoch:       goclient.FarFutureEpoch,
		}

		// Update the share with new metadata
		updatedShares, err := storage.Shares.UpdateValidatorsMetadata(
			map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
				testShare.ValidatorPubKey: updatedMetadata,
			})
		require.NoError(t, err)
		require.NotNil(t, updatedShares)
		reopen(t)

		// Ensure the updated share is reflected in validatorStore
		updatedShare, exists := storage.ValidatorStore.Validator(testShare.ValidatorPubKey[:])
		require.True(t, exists)
		require.NotNil(t, updatedShare, "Updated share should be present in validator store")
		require.Equal(t, updatedMetadata, &beaconprotocol.ValidatorMetadata{
			Status:          updatedShare.Status,
			Index:           updatedShare.ValidatorIndex,
			ActivationEpoch: updatedShare.ActivationEpoch,
			ExitEpoch:       updatedShare.ExitEpoch,
		}, "Validator metadata should be updated in validator store")

		// Remove the share via SharesStorage
		require.NoError(t, storage.Shares.Delete(nil, testShare.ValidatorPubKey[:]))
		reopen(t)

		// Verify that the share is removed from both sharesStorage and validatorStore
		deletedShare, exists := storage.Shares.Get(nil, testShare.ValidatorPubKey[:])
		require.False(t, exists)
		require.Nil(t, deletedShare, "Share should be deleted from sharesStorage")

		removedShare, exists := storage.ValidatorStore.Validator(testShare.ValidatorPubKey[:])
		require.False(t, exists)
		require.Nil(t, removedShare, "Share should be removed from validator store after deletion in sharesStorage")
	})
}

// Test various edge cases where operators have multiple committees.
func TestShareStorage_MultipleCommittees(t *testing.T) {
	testWithStorageReopen(t, func(t *testing.T, storage *testStorage, reopen func(t *testing.T)) {
		shares := map[phase0.ValidatorIndex]*ssvtypes.SSVShare{}
		saveAndVerify := func(s ...*ssvtypes.SSVShare) {
			require.NoError(t, storage.Shares.Save(nil, s...))
			reopen(t)
			for _, share := range s {
				shares[share.ValidatorIndex] = share
			}
			requireValidatorStoreIntegrity(t, storage.ValidatorStore, slices.Collect(maps.Values(shares)))
		}
		deleteAndVerify := func(share *ssvtypes.SSVShare) {
			require.NoError(t, storage.Shares.Delete(nil, share.ValidatorPubKey[:]))
			reopen(t)
			delete(shares, share.ValidatorIndex)
			requireValidatorStoreIntegrity(t, storage.ValidatorStore, slices.Collect(maps.Values(shares)))
		}

		share1 := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
		share2 := fakeParticipatingShare(2, generateRandomPubKey(), []uint64{1, 2, 3, 4})
		share3 := fakeParticipatingShare(3, generateRandomPubKey(), []uint64{3, 4, 5, 6})
		share4 := fakeParticipatingShare(4, generateRandomPubKey(), []uint64{9, 10, 11, 12})
		saveAndVerify(share1, share2, share3, share4)

		// Test that an exclusive committee with only 1 validator is removed then re-added
		// for operators that also have other committees (edgecase).
		deleteAndVerify(share3)
		saveAndVerify(share3)

		// Test that a committee with multiple validators is not removed.
		deleteAndVerify(share2)

		// Test that a committee with multiple validators is removed when all committee validators are removed.
		deleteAndVerify(share1)

		// Test that ValidatorStore is empty after all validators are removed.
		deleteAndVerify(share3)
		deleteAndVerify(share4)
		require.Empty(t, storage.ValidatorStore.Validators())
		require.Empty(t, storage.ValidatorStore.Committees())
		require.Empty(t, storage.ValidatorStore.OperatorValidators(1))

		// Re-add share2 to test that ValidatorStore is updated correctly.
		saveAndVerify(share2)
	})
}

func TestSharesStorage_HighContentionConcurrency(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	share1 := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share2 := fakeParticipatingShare(2, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share3 := fakeParticipatingShare(3, generateRandomPubKey(), []uint64{3, 4, 5, 6})
	share4 := fakeParticipatingShare(4, generateRandomPubKey(), []uint64{9, 10, 11, 12})

	// High contention test with concurrent read, add, update, and remove
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		for _, op := range []string{"add", "update", "remove1", "remove4", "read"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					switch op {
					case "add":
						require.NoError(t, storage.Shares.Save(nil, share1, share2, share3, share4))
					case "update":
						_, err := storage.Shares.UpdateValidatorsMetadata(
							map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
								share2.ValidatorPubKey: {
									Status:          updatedShare2.Status,
									Index:           updatedShare2.ValidatorIndex,
									ActivationEpoch: updatedShare2.ActivationEpoch,
									ExitEpoch:       updatedShare2.ExitEpoch},
							})
						require.NoError(t, err)
					case "remove1":
						require.NoError(t, storage.Shares.Delete(nil, share1.ValidatorPubKey[:]))
					case "remove4":
						require.NoError(t, storage.Shares.Delete(nil, share4.ValidatorPubKey[:]))
					case "read":
						_, _ = storage.ValidatorStore.Validator(share1.ValidatorPubKey[:])
						_, _ = storage.ValidatorStore.Committee(share1.CommitteeID())
						_ = storage.ValidatorStore.Validators()
						_ = storage.ValidatorStore.Committees()
					}
				}
			}()
		}
	}
	wg.Wait()

	t.Run("validate high contention state", func(t *testing.T) {
		// Check that the store is consistent and valid after high contention
		require.NotPanics(t, func() {
			storage.ValidatorStore.Validators()
			storage.ValidatorStore.Committees()
			storage.ValidatorStore.OperatorValidators(1)
			storage.ValidatorStore.OperatorCommittees(1)
		})

		// Verify that share2 and share3 are in the validator store (only share1 and share4 are potentially removed).
		share2InStore, exists := storage.ValidatorStore.ValidatorByIndex(share2.ValidatorIndex)
		require.True(t, exists)
		require.NotNil(t, share2InStore)
		requireEqualShare(t, share2, share2InStore)

		share3InStore, exists := storage.ValidatorStore.ValidatorByIndex(share3.ValidatorIndex)
		require.True(t, exists)
		require.NotNil(t, share3InStore)
		requireEqualShare(t, share3, share3InStore)

		// Integrity check.
		requireValidatorStoreIntegrity(t, storage.ValidatorStore, storage.Shares.List(nil))
	})
}

// Runs the given function as a test with and without storage reopen.
func testWithStorageReopen(t *testing.T, f func(t *testing.T, storage *testStorage, reopen func(t *testing.T))) {
	for _, withReopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("withReopen=%t", withReopen), func(t *testing.T) {
			logger := log.TestLogger(t)
			storage, err := newTestStorage(logger)
			require.NoError(t, err)
			defer storage.Close()

			reopen := func(t *testing.T) {
				if withReopen {
					require.NoError(t, storage.Reopen(logger))
				}
			}
			f(t, storage, reopen)
		})
	}
}

func requireEqualShare(t *testing.T, expected, actual *ssvtypes.SSVShare, msgAndArgs ...any) {
	b1, err := json.Marshal(expected)
	require.NoError(t, err)
	b2, err := json.Marshal(actual)
	require.NoError(t, err)
	require.JSONEq(t, string(b1), string(b2), msgAndArgs...)
}

func requireEqualShares(t *testing.T, expected, actual []*ssvtypes.SSVShare, msgAndArgs ...any) {
	require.Equal(t, len(expected), len(actual), msgAndArgs...)

	// Sort shares by validator pubkey for comparison without mutating input slices.
	expectedSorted := make([]*ssvtypes.SSVShare, len(expected))
	copy(expectedSorted, expected)
	slices.SortFunc(expectedSorted, func(a, b *ssvtypes.SSVShare) int {
		return strings.Compare(string(a.ValidatorPubKey[:]), string(b.ValidatorPubKey[:]))
	})

	actualSorted := make([]*ssvtypes.SSVShare, len(actual))
	copy(actualSorted, actual)
	slices.SortFunc(actualSorted, func(a, b *ssvtypes.SSVShare) int {
		return strings.Compare(string(a.ValidatorPubKey[:]), string(b.ValidatorPubKey[:]))
	})

	// Compare the sorted shares
	for i, share := range expectedSorted {
		requireEqualShare(t, share, actualSorted[i], msgAndArgs...)
	}
}

func generateRandomValidatorStorageShare(splitKeys map[uint64]*bls.SecretKey) *Share {
	sk1 := bls.SecretKey{}
	sk1.SetByCSPRNG()

	sk2 := bls.SecretKey{}
	sk2.SetByCSPRNG()

	ibftCommittee := make([]*storageOperator, 0, len(splitKeys))
	for operatorID, sk := range splitKeys {
		ibftCommittee = append(ibftCommittee, &storageOperator{
			OperatorID: operatorID,
			PubKey:     sk.Serialize(),
		})
	}
	sort.Slice(ibftCommittee, func(i, j int) bool {
		return ibftCommittee[i].OperatorID < ibftCommittee[j].OperatorID
	})

	return &Share{
		ValidatorIndex:  3,
		ValidatorPubKey: sk1.GetPublicKey().Serialize(),
		SharePubKey:     sk2.GetPublicKey().Serialize(),
		Committee:       ibftCommittee,
		DomainType:      networkconfig.TestNetwork.DomainType,
		Graffiti:        bytes.Repeat([]byte{0x01}, 32),
		Status:          2,
		ActivationEpoch: 4,
		ExitEpoch:       5,
		OwnerAddress:    common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
		Liquidated:      true,
	}
}

func generateRandomShare(splitKeys map[uint64]*bls.SecretKey, state v1.ValidatorState, isLiquidated bool) *ssvtypes.SSVShare {
	sk1 := bls.SecretKey{}
	sk1.SetByCSPRNG()

	sk2 := bls.SecretKey{}
	sk2.SetByCSPRNG()

	ibftCommittee := make([]*spectypes.ShareMember, 0, len(splitKeys))
	for operatorID, sk := range splitKeys {
		ibftCommittee = append(ibftCommittee, &spectypes.ShareMember{
			Signer:      operatorID,
			SharePubKey: sk.Serialize(),
		})
	}
	sort.Slice(ibftCommittee, func(i, j int) bool {
		return ibftCommittee[i].Signer < ibftCommittee[j].Signer
	})

	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  phase0.ValidatorIndex(rand.Uint64()),
			ValidatorPubKey: spectypes.ValidatorPK(sk1.GetPublicKey().Serialize()),
			SharePubKey:     sk2.GetPublicKey().Serialize(),
			Committee:       ibftCommittee,
			DomainType:      networkconfig.TestNetwork.DomainType,
			Graffiti:        bytes.Repeat([]byte{0x01}, 32),
		},
		Status:                    state,
		ActivationEpoch:           phase0.Epoch(rand.Uint64()),
		ExitEpoch:                 phase0.Epoch(rand.Uint64()),
		OwnerAddress:              common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
		Liquidated:                isLiquidated,
		BeaconMetadataLastUpdated: time.Now(),
	}
}

func generateRandomPubKey() spectypes.ValidatorPK {
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	return spectypes.ValidatorPK(sk.GetPublicKey().Serialize())
}

func fakeParticipatingShare(index phase0.ValidatorIndex, pk spectypes.ValidatorPK, operatorIDs []uint64) *ssvtypes.SSVShare {
	committee := make([]*spectypes.ShareMember, len(operatorIDs))
	for i, operatorID := range operatorIDs {
		sharePubkey := make(spectypes.ShareValidatorPK, len(pk))
		spk := generateRandomPubKey()
		copy(sharePubkey, spk[:])

		committee[i] = &spectypes.ShareMember{
			Signer:      operatorID,
			SharePubKey: sharePubkey,
		}
	}

	// Use a unique owner address for each validator to avoid conflicts in tests
	ownerAddr := common.HexToAddress(fmt.Sprintf("0x%040x", index))

	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: pk,
			ValidatorIndex:  index,
			SharePubKey:     committee[0].SharePubKey,
			Committee:       committee,
			DomainType:      networkconfig.TestNetwork.DomainType,
			Graffiti:        bytes.Repeat([]byte{0x01}, 32),
		},
		Status:          v1.ValidatorStateActiveOngoing,
		ActivationEpoch: 4,
		ExitEpoch:       goclient.FarFutureEpoch,
		OwnerAddress:    ownerAddr,
		Liquidated:      false,
	}
}

func generateMaxPossibleShare() (*Share, error) {
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	const keysCount = 13

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	if err != nil {
		return nil, err
	}

	validatorShare := generateRandomValidatorStorageShare(splitKeys)
	return validatorShare, nil
}

// noOpRecipientReader is a no-op implementation of RecipientReader for tests
// that don't need fee recipient functionality
type noOpRecipientReader struct{}

func (n *noOpRecipientReader) GetRecipientData(_ basedb.Reader, _ common.Address) (*RecipientData, bool, error) {
	return nil, false, nil
}

func (n *noOpRecipientReader) GetRecipientDataMany(_ basedb.Reader, _ []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	return make(map[common.Address]bellatrix.ExecutionAddress), nil
}

type testStorage struct {
	db             *kv.DB
	Operators      Operators
	Shares         Shares
	ValidatorStore ValidatorStore
	Recipients     Recipients
}

func newTestStorage(logger *zap.Logger) (*testStorage, error) {
	db, err := kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return nil, err
	}
	s := &testStorage{db: db}
	if err := s.open(logger); err != nil {
		return nil, err
	}
	return s, nil
}

func (t *testStorage) open(logger *zap.Logger) error {
	var err error
	// Create recipient storage and wire it up with shares storage
	var setSharesUpdater func(SharesUpdater)
	t.Recipients, setSharesUpdater = NewRecipientsStorage(logger, t.db, []byte("test"))
	t.Shares, t.ValidatorStore, err = NewSharesStorage(networkconfig.TestNetwork.Beacon, t.db, t.Recipients, []byte("test"))
	if err != nil {
		return err
	}
	setSharesUpdater(t.Shares)
	t.Operators = NewOperatorsStorage(logger, t.db, []byte("test"))
	return nil
}

func (t *testStorage) Reopen(logger *zap.Logger) error {
	return t.open(logger)
}

func (t *testStorage) Recreate(logger *zap.Logger) error {
	err := t.Close()
	if err != nil {
		return err
	}
	t.db, err = kv.NewInMemory(logger, basedb.Options{})
	if err != nil {
		return err
	}
	return t.open(logger)
}

func (t *testStorage) Close() error {
	return t.db.Close()
}

// TestFeeRecipientEnrichment tests that fee recipients are properly enriched from recipients storage
func TestFeeRecipientEnrichment(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	owner1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	owner2 := common.HexToAddress("0x2345678901234567890123456789012345678901")
	feeRecipient1 := common.HexToAddress("0xAAAA567890123456789012345678901234567890")
	feeRecipient2 := common.HexToAddress("0xBBBB678901234567890123456789012345678901")

	// Create shares with different owners
	share1 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  1,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key1")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner1,
	}

	share2 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  2,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key2")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner2,
	}

	// Save shares - fee recipients should not be persisted
	require.NoError(t, storage.Shares.Save(nil, share1, share2))

	// Set custom fee recipients
	var feeRecipientAddr1, feeRecipientAddr2 bellatrix.ExecutionAddress
	copy(feeRecipientAddr1[:], feeRecipient1.Bytes())
	copy(feeRecipientAddr2[:], feeRecipient2.Bytes())

	_, err = storage.Recipients.SaveRecipientData(nil, &RecipientData{
		Owner:        owner1,
		FeeRecipient: feeRecipientAddr1,
	})
	require.NoError(t, err)

	_, err = storage.Recipients.SaveRecipientData(nil, &RecipientData{
		Owner:        owner2,
		FeeRecipient: feeRecipientAddr2,
	})
	require.NoError(t, err)

	// Reopen storage to trigger enrichment on load
	require.NoError(t, storage.Reopen(logger))

	// Verify shares are enriched with correct fee recipients
	loadedShare1, found := storage.Shares.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(feeRecipientAddr1), loadedShare1.FeeRecipientAddress)

	loadedShare2, found := storage.Shares.Get(nil, share2.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(feeRecipientAddr2), loadedShare2.FeeRecipientAddress)
}

// TestFeeRecipientDefaultsToOwner tests that fee recipient defaults to owner when no custom recipient exists
func TestFeeRecipientDefaultsToOwner(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	owner := common.HexToAddress("0x1234567890123456789012345678901234567890")

	share := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  1,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key1")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	// Save share without setting custom fee recipient
	require.NoError(t, storage.Shares.Save(nil, share))

	// Reopen storage to trigger enrichment
	require.NoError(t, storage.Reopen(logger))

	// Verify fee recipient defaults to owner address
	loadedShare, found := storage.Shares.Get(nil, share.ValidatorPubKey[:])
	require.True(t, found)
	var expectedFeeRecipient [20]byte
	copy(expectedFeeRecipient[:], owner.Bytes())
	require.Equal(t, expectedFeeRecipient, loadedShare.FeeRecipientAddress)
}

// TestUpdateFeeRecipientForOwner tests updating fee recipients for all shares of an owner
func TestUpdateFeeRecipientForOwner(t *testing.T) {
	logger := log.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
	newFeeRecipient := common.HexToAddress("0xAAAA567890123456789012345678901234567890")

	// Create multiple shares for the same owner
	share1 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  1,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key1")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	share2 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey: generateRandomPubKey(),
			ValidatorIndex:  2,
			Committee:       []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key2")}},
			DomainType:      networkconfig.TestNetwork.DomainType,
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	// Save shares
	require.NoError(t, storage.Shares.Save(nil, share1, share2))

	// Update fee recipient for owner
	var newFeeRecipientAddr bellatrix.ExecutionAddress
	copy(newFeeRecipientAddr[:], newFeeRecipient.Bytes())
	storage.Shares.UpdateFeeRecipientForOwner(owner, newFeeRecipientAddr)

	// Verify all shares for owner have updated fee recipient
	loadedShare1, found := storage.Shares.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(newFeeRecipientAddr), loadedShare1.FeeRecipientAddress)

	loadedShare2, found := storage.Shares.Get(nil, share2.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(newFeeRecipientAddr), loadedShare2.FeeRecipientAddress)
}

// TestOwnerIndexCleanupOnDelete tests that the owner index is properly maintained when shares are deleted
func TestOwnerIndexCleanupOnDelete(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	recipientStorage, setSharesUpdater := NewRecipientsStorage(logger, db, []byte("test-recipient"))
	sharesStorage, _, err := NewSharesStorage(networkconfig.TestNetwork.Beacon, db, recipientStorage, []byte("test"))
	require.NoError(t, err)
	setSharesUpdater(sharesStorage)

	owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
	customFeeRecipient := bellatrix.ExecutionAddress{0x11, 0x22, 0x33}

	// Create 3 shares for the same owner
	share1 := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share1.OwnerAddress = owner
	share2 := fakeParticipatingShare(2, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share2.OwnerAddress = owner
	share3 := fakeParticipatingShare(3, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share3.OwnerAddress = owner

	// Save all shares
	require.NoError(t, sharesStorage.Save(nil, share1))
	require.NoError(t, sharesStorage.Save(nil, share2))
	require.NoError(t, sharesStorage.Save(nil, share3))

	// Set custom fee recipient for owner
	_, err = recipientStorage.SaveRecipientData(nil, &RecipientData{
		Owner:        owner,
		FeeRecipient: customFeeRecipient,
	})
	require.NoError(t, err)

	// Verify all 3 shares have the custom fee recipient
	loaded1, found := sharesStorage.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(customFeeRecipient), loaded1.FeeRecipientAddress)

	loaded2, found := sharesStorage.Get(nil, share2.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(customFeeRecipient), loaded2.FeeRecipientAddress)

	loaded3, found := sharesStorage.Get(nil, share3.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(customFeeRecipient), loaded3.FeeRecipientAddress)

	// Delete share2
	require.NoError(t, sharesStorage.Delete(nil, share2.ValidatorPubKey[:]))

	// Verify share2 is deleted
	_, found = sharesStorage.Get(nil, share2.ValidatorPubKey[:])
	require.False(t, found)

	// Update fee recipient again
	newFeeRecipient := bellatrix.ExecutionAddress{0x44, 0x55, 0x66}
	_, err = recipientStorage.SaveRecipientData(nil, &RecipientData{
		Owner:        owner,
		FeeRecipient: newFeeRecipient,
	})
	require.NoError(t, err)

	// Verify only share1 and share3 are updated (share2 should not be in the index anymore)
	loaded1, found = sharesStorage.Get(nil, share1.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(newFeeRecipient), loaded1.FeeRecipientAddress, "Share1 should have new fee recipient")

	loaded3, found = sharesStorage.Get(nil, share3.ValidatorPubKey[:])
	require.True(t, found)
	require.Equal(t, [20]byte(newFeeRecipient), loaded3.FeeRecipientAddress, "Share3 should have new fee recipient")

	// Delete all remaining shares
	require.NoError(t, sharesStorage.Delete(nil, share1.ValidatorPubKey[:]))
	require.NoError(t, sharesStorage.Delete(nil, share3.ValidatorPubKey[:]))

	// Update fee recipient one more time
	finalFeeRecipient := bellatrix.ExecutionAddress{0x77, 0x88, 0x99}
	_, err = recipientStorage.SaveRecipientData(nil, &RecipientData{
		Owner:        owner,
		FeeRecipient: finalFeeRecipient,
	})
	require.NoError(t, err)

	// No shares should be updated since all were deleted
	// This verifies the owner index was properly cleaned up
	// (If the index wasn't cleaned, it would try to update deleted shares and potentially panic)
}

// TestFeeRecipientsNotPersisted tests that fee recipients are not persisted in shares storage
func TestFeeRecipientsNotPersisted(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	// Create shares storage
	recipients, setSharesUpdater := NewRecipientsStorage(logger, db, []byte("test"))
	shares, _, err := NewSharesStorage(networkconfig.TestNetwork.Beacon, db, recipients, []byte("test"))
	require.NoError(t, err)
	setSharesUpdater(shares)

	owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
	feeRecipient := common.HexToAddress("0xAAAA567890123456789012345678901234567890")

	share := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey:     generateRandomPubKey(),
			ValidatorIndex:      1,
			Committee:           []*spectypes.ShareMember{{Signer: 1, SharePubKey: []byte("key1")}},
			DomainType:          networkconfig.TestNetwork.DomainType,
			FeeRecipientAddress: feeRecipient, // Set fee recipient
		},
		Status:       v1.ValidatorStateActiveOngoing,
		OwnerAddress: owner,
	}

	// Save share
	require.NoError(t, shares.Save(nil, share))

	// Read raw storage data
	// Build the prefix and key
	prefix := SharesDBPrefix([]byte("test"))

	obj, found, err := db.Get(prefix, share.ValidatorPubKey[:])
	require.NoError(t, err)
	require.True(t, found)

	// Decode storage share
	var storageShare Share
	require.NoError(t, storageShare.Decode(obj.Value))

	// Verify fee recipient is NOT persisted (should be zeros)
	require.Equal(t, [20]byte{}, storageShare.FeeRecipientAddress)
}
