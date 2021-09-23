package beacon

import (
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"sync/atomic"
	"testing"
)

func init() {
	logex.Build("test", zap.InfoLevel, nil)
}

func TestValidatorMetadata_Status(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateActiveOngoing,
		}
		require.True(t, meta.Deposited())
		require.False(t, meta.Exiting())
		require.False(t, meta.Slashed())
	})

	t.Run("exited", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateWithdrawalPossible,
		}
		require.True(t, meta.Exiting())
		require.True(t, meta.Deposited())
		require.False(t, meta.Slashed())
	})

	t.Run("exitedSlashed", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: v1.ValidatorStateExitedSlashed,
		}
		require.True(t, meta.Slashed())
		require.True(t, meta.Exiting())
		require.True(t, meta.Deposited())
	})
}

func TestUpdateValidatorsMetadata(t *testing.T) {
	var updateCount uint64
	pks := []string{
		"a17bb48a3f8f558e29d08ede97d6b7b73823d8dc2e0530fe8b747c93d7d6c2755957b7ffb94a7cec830456fd5492ba19",
		"a0cf5642ed5aa82178a5f79e00292c5b700b67fbf59630ce4f542c392495d9835a99c826aa2459a67bc80867245386c6",
	}

	pk1 := bls.PublicKey{}
	require.Nil(t, pk1.DeserializeHexStr(pks[0]))
	blsPubKey1 := spec.BLSPubKey{}
	copy(blsPubKey1[:], pk1.Serialize())

	pk2 := bls.PublicKey{}
	require.Nil(t, pk2.DeserializeHexStr(pks[1]))
	blsPubKey2 := spec.BLSPubKey{}
	copy(blsPubKey2[:], pk2.Serialize())

	data := map[spec.BLSPubKey]*v1.Validator{}
	data[blsPubKey1] = &v1.Validator{
		Index:     spec.ValidatorIndex(210961),
		Status:    v1.ValidatorStateWithdrawalPossible,
		Validator: &spec.Validator{PublicKey: blsPubKey1},
	}
	data[blsPubKey2] = &v1.Validator{
		Index:     spec.ValidatorIndex(213820),
		Status:    v1.ValidatorStateActiveOngoing,
		Validator: &spec.Validator{PublicKey: blsPubKey2},
	}
	bc := NewMockBeacon(map[uint64][]*Duty{}, data)

	storage := NewMockValidatorMetadataStorage()

	onUpdated := func(pk string, meta *ValidatorMetadata) {
		joined := strings.Join(pks, ":")
		require.True(t, strings.Contains(joined, pk))
		require.True(t, meta.Index == spec.ValidatorIndex(210961) || meta.Index == spec.ValidatorIndex(213820))
		atomic.AddUint64(&updateCount, 1)
	}
	err := UpdateValidatorsMetadata([][]byte{pk1.Serialize(), pk2.Serialize()}, storage, bc, onUpdated)
	require.Nil(t, err)
	require.Equal(t, uint64(2), updateCount)
	require.Equal(t, 2, storage.(*mockValidatorMetadataStorage).Size())
}
