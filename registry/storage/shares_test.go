package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"math/rand"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
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
	logger := logging.TestLogger(t)
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
		_, err = storage.Operators.SaveOperatorData(nil, &OperatorData{ID: operatorID, PublicKey: []byte(strconv.FormatUint(operatorID, 10))})
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

	t.Run("UpdateValidatorMetadata_updatesMetadata", func(t *testing.T) {
		for _, share := range persistedActiveValidatorShares {
			updatedIndex := phase0.ValidatorIndex(rand.Uint64())
			updatedActivationEpoch, updatedExitEpoch := phase0.Epoch(5), phase0.Epoch(6)
			updatedStatus := v1.ValidatorStateActiveOngoing

			err := storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
				share.ValidatorPubKey: {
					Index:           updatedIndex,
					Status:          updatedStatus,
					ActivationEpoch: updatedActivationEpoch,
					ExitEpoch:       updatedExitEpoch,
				}})
			require.NoError(t, err)

			fetchedShare, exists := storage.Shares.Get(nil, share.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, fetchedShare)
			require.Equal(t, updatedIndex, fetchedShare.ValidatorIndex)
			require.Equal(t, updatedActivationEpoch, fetchedShare.ActivationEpoch)
			require.Equal(t, updatedExitEpoch, fetchedShare.ExitEpoch)
			require.Equal(t, updatedStatus, fetchedShare.Status)
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

	t.Run("List_Filter_ByAttesting", func(t *testing.T) {
		const epoch = 1
		var attestingShares int
		for _, shares := range persistedActiveValidatorShares {
			if shares.IsAttesting(epoch) {
				attestingShares++
			}
		}
		validators := storage.Shares.List(nil, ByAttesting(epoch))
		require.Equal(t, attestingShares, len(validators))
	})

	t.Run("KV_reuse_works", func(t *testing.T) {
		storageDuplicate, _, err := NewSharesStorage(networkconfig.TestNetwork, storage.db, []byte("test"))
		require.NoError(t, err)
		existingValidators := storageDuplicate.List(nil)

		require.Equal(t, 2, len(existingValidators))
	})
}

func TestShareDeletionHandlesValidatorStoreCorrectly(t *testing.T) {
	logger := logging.TestLogger(t)

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
			Status:          v1.ValidatorStateActiveOngoing,
			Index:           3,
			ActivationEpoch: 5,
			ExitEpoch:       goclient.FarFutureEpoch,
		}

		// Update the share with new metadata
		require.NoError(t, storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
			testShare.ValidatorPubKey: updatedMetadata,
		}))
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
	logger := logging.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	share1 := fakeParticipatingShare(1, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share2 := fakeParticipatingShare(2, generateRandomPubKey(), []uint64{1, 2, 3, 4})
	share3 := fakeParticipatingShare(3, generateRandomPubKey(), []uint64{3, 4, 5, 6})
	share4 := fakeParticipatingShare(4, generateRandomPubKey(), []uint64{9, 10, 11, 12})

	// High contention test with concurrent read, add, update, and remove
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
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
						require.NoError(t, storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
							share2.ValidatorPubKey: {
								Status:          updatedShare2.Status,
								Index:           updatedShare2.ValidatorIndex,
								ActivationEpoch: updatedShare2.ActivationEpoch,
								ExitEpoch:       updatedShare2.ExitEpoch,
							},
						}))
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
		withReopen := withReopen
		t.Run(fmt.Sprintf("withReopen=%t", withReopen), func(t *testing.T) {
			logger := logging.TestLogger(t)
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

	var ibftCommittee []*storageOperator
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
		ValidatorIndex:      3,
		ValidatorPubKey:     sk1.GetPublicKey().Serialize(),
		SharePubKey:         sk2.GetPublicKey().Serialize(),
		Committee:           ibftCommittee,
		DomainType:          networkconfig.TestNetwork.DomainType,
		FeeRecipientAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
		Graffiti:            bytes.Repeat([]byte{0x01}, 32),
		Status:              2,
		ActivationEpoch:     4,
		ExitEpoch:           5,
		OwnerAddress:        common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
		Liquidated:          true,
	}
}

func generateRandomShare(splitKeys map[uint64]*bls.SecretKey, state v1.ValidatorState, isLiquidated bool) *ssvtypes.SSVShare {
	sk1 := bls.SecretKey{}
	sk1.SetByCSPRNG()

	sk2 := bls.SecretKey{}
	sk2.SetByCSPRNG()

	var ibftCommittee []*spectypes.ShareMember
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
			ValidatorIndex:      phase0.ValidatorIndex(rand.Uint64()),
			ValidatorPubKey:     spectypes.ValidatorPK(sk1.GetPublicKey().Serialize()),
			SharePubKey:         sk2.GetPublicKey().Serialize(),
			Committee:           ibftCommittee,
			DomainType:          networkconfig.TestNetwork.DomainType,
			FeeRecipientAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Graffiti:            bytes.Repeat([]byte{0x01}, 32),
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

	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorPubKey:     pk,
			ValidatorIndex:      index,
			SharePubKey:         committee[0].SharePubKey,
			Committee:           committee,
			DomainType:          networkconfig.TestNetwork.DomainType,
			FeeRecipientAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Graffiti:            bytes.Repeat([]byte{0x01}, 32),
		},
		Status:          v1.ValidatorStateActiveOngoing,
		ActivationEpoch: 4,
		ExitEpoch:       goclient.FarFutureEpoch,
		OwnerAddress:    common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
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

type testStorage struct {
	db             *kv.BadgerDB
	Operators      Operators
	Shares         Shares
	ValidatorStore ValidatorStore
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
	t.Shares, t.ValidatorStore, err = NewSharesStorage(networkconfig.TestNetwork, t.db, []byte("test"))
	if err != nil {
		return err
	}
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
