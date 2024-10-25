package metadata

import (
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/operator/validator/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// This test is copied from beacon package (TestUpdateValidatorsMetadata) and may require further refactoring.
func TestFetcher(t *testing.T) {
	logger := logging.TestLogger(t)

	var updateCount uint64
	pks := []string{
		"a17bb48a3f8f558e29d08ede97d6b7b73823d8dc2e0530fe8b747c93d7d6c2755957b7ffb94a7cec830456fd5492ba19",
		"a0cf5642ed5aa82178a5f79e00292c5b700b67fbf59630ce4f542c392495d9835a99c826aa2459a67bc80867245386c6",
		"8bafb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
	}

	decodeds := make([][]byte, len(pks))
	blsPubKeys := make([]phase0.BLSPubKey, len(pks))
	for i, pk := range pks {
		blsPubKey := phase0.BLSPubKey{}
		decoded, _ := hex.DecodeString(pk)
		copy(blsPubKey[:], decoded)
		decodeds[i] = decoded
		blsPubKeys[i] = blsPubKey
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := beacon.NewMockBeaconNode(ctrl)
	bc.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		validatorsData := map[phase0.BLSPubKey]*eth2apiv1.Validator{
			blsPubKeys[0]: {
				Index:     phase0.ValidatorIndex(210961),
				Status:    eth2apiv1.ValidatorStateWithdrawalPossible,
				Validator: &phase0.Validator{PublicKey: blsPubKeys[0]},
			},
			blsPubKeys[1]: {
				Index:     phase0.ValidatorIndex(213820),
				Status:    eth2apiv1.ValidatorStateActiveOngoing,
				Validator: &phase0.Validator{PublicKey: blsPubKeys[1]},
			},
		}

		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for _, pk := range validatorPubKeys {
			if data, ok := validatorsData[pk]; ok {
				results[data.Index] = data
			}
		}
		return results, nil
	})

	storageData := make(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata)
	storageMu := sync.Mutex{}

	storage := mocks.NewMockSharesStorage(ctrl)

	storage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).DoAndReturn(func(metadata map[spectypes.ValidatorPK]*beacon.ValidatorMetadata) error {
		storageMu.Lock()
		defer storageMu.Unlock()

		for pk, meta := range metadata {
			storageData[pk] = meta
		}

		return nil
	}).AnyTimes()

	mf := NewFetcher(logger, bc)
	pubKeys := []spectypes.ValidatorPK{
		spectypes.ValidatorPK(blsPubKeys[0]),
		spectypes.ValidatorPK(blsPubKeys[1]),
		spectypes.ValidatorPK(blsPubKeys[2]),
	}
	results, err := mf.Fetch(pubKeys)
	require.NoError(t, err)

	require.NoError(t, storage.UpdateValidatorsMetadata(results))
	for pk, meta := range results {
		joined := strings.Join(pks, ":")

		require.True(t, strings.Contains(joined, strings.Trim(phase0.BLSPubKey(pk).String(), "0x")))
		require.True(t, meta.Index == phase0.ValidatorIndex(210961) || meta.Index == phase0.ValidatorIndex(213820))
		atomic.AddUint64(&updateCount, 1)
	}

	require.Equal(t, uint64(2), updateCount)

	storageMu.Lock()
	storageSize := len(storageData)
	storageMu.Unlock()
	require.Equal(t, 2, storageSize)
}
