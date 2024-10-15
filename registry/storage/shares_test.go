package storage

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/threshold"
)

func TestValidatorSerializer(t *testing.T) {
	threshold.Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	const keysCount = 13

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	validatorShare, _ := generateRandomValidatorStorageShare(splitKeys)
	b, err := validatorShare.Encode()
	require.NoError(t, err)

	obj := basedb.Obj{
		Key:   validatorShare.ValidatorPubKey[:],
		Value: b,
	}
	v1 := &storageShare{}
	require.NoError(t, v1.Decode(obj.Value))
	require.NotNil(t, v1.ValidatorPubKey)
	require.Equal(t, hex.EncodeToString(v1.ValidatorPubKey[:]), hex.EncodeToString(validatorShare.ValidatorPubKey[:]))
	require.NotNil(t, v1.Committee)
	require.NotNil(t, v1.OperatorID)
	require.Equal(t, v1.BeaconMetadata, validatorShare.BeaconMetadata)
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

	validatorShare, _ := generateRandomValidatorSpecShare(splitKeys)
	validatorShare.Metadata = ssvtypes.Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Balance:         1,
			Status:          eth2apiv1.ValidatorStateActiveOngoing,
			Index:           3,
			ActivationEpoch: 4,
		},
		OwnerAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
		Liquidated:   false,
	}
	require.NoError(t, storage.Shares.Save(nil, validatorShare))

	validatorShare2, _ := generateRandomValidatorSpecShare(splitKeys)
	require.NoError(t, storage.Shares.Save(nil, validatorShare2))

	validatorShareByKey, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
	require.True(t, exists)
	require.NotNil(t, validatorShareByKey)
	require.NoError(t, err)
	require.EqualValues(t, hex.EncodeToString(validatorShareByKey.ValidatorPubKey[:]), hex.EncodeToString(validatorShare.ValidatorPubKey[:]))
	require.EqualValues(t, validatorShare.Committee, validatorShareByKey.Committee)

	validators := storage.Shares.List(nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, len(validators))

	t.Run("UpdateValidatorMetadata_shareExists", func(t *testing.T) {
		require.NoError(t, storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
			validatorShare.ValidatorPubKey: {
				Balance:         10000,
				Index:           3,
				Status:          eth2apiv1.ValidatorStateActiveOngoing,
				ActivationEpoch: 4,
			},
		}))
	})

	t.Run("List_Filter_ByClusterId", func(t *testing.T) {
		clusterID := ssvtypes.ComputeClusterIDHash(validatorShare.Metadata.OwnerAddress, []uint64{1, 2, 3, 4})

		validators := storage.Shares.List(nil, ByClusterIDHash(clusterID))
		require.Equal(t, 2, len(validators))
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
		validators := storage.Shares.List(nil, ByAttesting(phase0.Epoch(1)))
		require.Equal(t, 1, len(validators))
	})

	t.Run("KV_reuse_works", func(t *testing.T) {
		storageDuplicate, _, err := NewSharesStorage(logger, storage.db, []byte("test"))
		require.NoError(t, err)
		existingValidators := storageDuplicate.List(nil)

		require.Equal(t, 2, len(existingValidators))
	})

	require.NoError(t, storage.Shares.Delete(nil, validatorShare.ValidatorPubKey[:]))
	share, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
	require.False(t, exists)
	require.Nil(t, share)

	t.Run("UpdateValidatorMetadata_shareIsDeleted", func(t *testing.T) {
		require.NoError(t, storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
			validatorShare.ValidatorPubKey: {
				Balance:         10000,
				Index:           3,
				Status:          2,
				ActivationEpoch: 4,
			},
		}))
	})

	t.Run("Drop", func(t *testing.T) {
		require.NoError(t, storage.Shares.Drop())

		validators := storage.Shares.List(nil, ByOperatorID(1))
		require.NoError(t, err)
		require.EqualValues(t, 0, len(validators))
	})
}

func generateRandomValidatorStorageShare(splitKeys map[uint64]*bls.SecretKey) (*storageShare, *bls.SecretKey) {
	threshold.Init()

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

	quorum, partialQuorum := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(splitKeys)))

	return &storageShare{
		Share: Share{
			OperatorID:          1,
			ValidatorPubKey:     sk1.GetPublicKey().Serialize(),
			SharePubKey:         sk2.GetPublicKey().Serialize(),
			Committee:           ibftCommittee,
			Quorum:              quorum,
			PartialQuorum:       partialQuorum,
			DomainType:          networkconfig.TestNetwork.DomainType(),
			FeeRecipientAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Graffiti:            bytes.Repeat([]byte{0x01}, 32),
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Balance:         1,
				Status:          2,
				Index:           3,
				ActivationEpoch: 4,
			},
			OwnerAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Liquidated:   true,
		},
	}, &sk1
}

func generateRandomValidatorSpecShare(splitKeys map[uint64]*bls.SecretKey) (*ssvtypes.SSVShare, *bls.SecretKey) {
	threshold.Init()

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
			ValidatorPubKey:     spectypes.ValidatorPK(sk1.GetPublicKey().Serialize()),
			SharePubKey:         sk2.GetPublicKey().Serialize(),
			Committee:           ibftCommittee,
			DomainType:          networkconfig.TestNetwork.DomainType(),
			FeeRecipientAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Graffiti:            bytes.Repeat([]byte{0x01}, 32),
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Balance:         1,
				Status:          2,
				Index:           3,
				ActivationEpoch: 4,
			},
			OwnerAddress: common.HexToAddress("0xFeedB14D8b2C76FdF808C29818b06b830E8C2c0e"),
			Liquidated:   true,
		},
	}, &sk1
}

func generateMaxPossibleShare() (*storageShare, error) {
	threshold.Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	const keysCount = 13

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	if err != nil {
		return nil, err
	}

	validatorShare, _ := generateRandomValidatorStorageShare(splitKeys)
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
	t.Shares, t.ValidatorStore, err = NewSharesStorage(logger, t.db, []byte("test"))
	if err != nil {
		return err
	}
	t.Operators = NewOperatorsStorage(logger, t.db, []byte("test"))
	return nil
}

func (t *testStorage) Reopen(logger *zap.Logger) error {
	return t.open(logger)
}

func (t *testStorage) Close() error {
	return t.db.Close()
}

func TestShareDeletionHandlesValidatorStoreCorrectly(t *testing.T) {
	logger := logging.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	// Initialize threshold and generate keys for test setup
	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	// Save operators to the storage
	for operatorID := range splitKeys {
		_, err = storage.Operators.SaveOperatorData(nil, &OperatorData{ID: operatorID, PublicKey: []byte(strconv.FormatUint(operatorID, 10))})
		require.NoError(t, err)
	}

	// Test share deletion with and without reopening the database.
	for _, withReopen := range []bool{true, false} {
		t.Run(fmt.Sprintf("withReopen=%t", withReopen), func(t *testing.T) {
			// Generate and save a random validator share
			validatorShare, _ := generateRandomValidatorSpecShare(splitKeys)
			require.NoError(t, storage.Shares.Save(nil, validatorShare))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Ensure the share is saved correctly
			savedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, savedShare)

			// Ensure the share is saved correctly in the validatorStore
			validatorShareFromStore, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, validatorShareFromStore)

			// Delete the share from storage
			require.NoError(t, storage.Shares.Delete(nil, validatorShare.ValidatorPubKey[:]))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Verify that the share is deleted from shareStorage
			deletedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
			require.False(t, exists)
			require.Nil(t, deletedShare, "Share should be deleted from shareStorage")

			// Verify that the validatorStore reflects the removal correctly
			removedShare, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
			require.False(t, exists)
			require.Nil(t, removedShare, "Share should be removed from validator store after deletion")

			// Further checks on internal data structures
			committeeID := validatorShare.CommitteeID()
			committee, exists := storage.ValidatorStore.Committee(committeeID)
			require.False(t, exists)
			require.Nil(t, committee, "Committee should be nil after share deletion")

			// Verify that other internal mappings are updated accordingly
			for _, operator := range validatorShare.Committee {
				shares := storage.ValidatorStore.OperatorValidators(operator.Signer)
				require.Empty(t, shares, "Data for operator should be nil after share deletion")
			}

			// Cleanup the share storage for the next test
			require.NoError(t, storage.Shares.Drop())
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}
			validators := storage.Shares.List(nil)
			require.EqualValues(t, 0, len(validators), "No validators should be left in storage after drop")
		})
	}
}

func TestValidatorStoreThroughSharesStorage(t *testing.T) {
	logger := logging.TestLogger(t)
	storage, err := newTestStorage(logger)
	require.NoError(t, err)
	defer storage.Close()

	// Initialize threshold and generate keys for test setup
	threshold.Init()
	const keysCount = 4

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	splitKeys, err := threshold.Create(sk.Serialize(), keysCount-1, keysCount)
	require.NoError(t, err)

	// Save operators to the storage
	for operatorID := range splitKeys {
		_, err = storage.Operators.SaveOperatorData(nil, &OperatorData{ID: operatorID, PublicKey: []byte(strconv.FormatUint(operatorID, 10))})
		require.NoError(t, err)
	}

	for _, withReopen := range []bool{true, false} {
		t.Run(fmt.Sprintf("withReopen=%t", withReopen), func(t *testing.T) {
			// Generate and save a random validator share
			validatorShare, _ := generateRandomValidatorSpecShare(splitKeys)
			require.NoError(t, storage.Shares.Save(nil, validatorShare))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Try saving nil share/shares
			require.Error(t, storage.Shares.Save(nil, nil))
			require.Error(t, storage.Shares.Save(nil, nil, validatorShare))
			require.Error(t, storage.Shares.Save(nil, validatorShare, nil))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Ensure the share is saved correctly
			savedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, savedShare)

			// Verify that the validatorStore has the share via SharesStorage
			storedShare, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, storedShare, "Share should be present in validator store after adding to sharesStorage")

			// Now update the share
			updatedMetadata := &beaconprotocol.ValidatorMetadata{
				Balance:         5000,
				Status:          eth2apiv1.ValidatorStateActiveOngoing,
				Index:           3,
				ActivationEpoch: 5,
			}

			// Update the share with new metadata
			require.NoError(t, storage.Shares.UpdateValidatorsMetadata(map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata{
				validatorShare.ValidatorPubKey: updatedMetadata,
			}))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Ensure the updated share is reflected in validatorStore
			updatedShare, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
			require.True(t, exists)
			require.NotNil(t, updatedShare, "Updated share should be present in validator store")
			require.Equal(t, updatedMetadata, updatedShare.BeaconMetadata, "Validator metadata should be updated in validator store")

			// Remove the share via SharesStorage
			require.NoError(t, storage.Shares.Delete(nil, validatorShare.ValidatorPubKey[:]))
			if withReopen {
				require.NoError(t, storage.Reopen(logger))
			}

			// Verify that the share is removed from both sharesStorage and validatorStore
			deletedShare, exists := storage.Shares.Get(nil, validatorShare.ValidatorPubKey[:])
			require.False(t, exists)
			require.Nil(t, deletedShare, "Share should be deleted from sharesStorage")

			removedShare, exists := storage.ValidatorStore.Validator(validatorShare.ValidatorPubKey[:])
			require.False(t, exists)
			require.Nil(t, removedShare, "Share should be removed from validator store after deletion in sharesStorage")
		})
	}
}
