package web3signer

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

func TestConvertBlockToBeaconBlockData(t *testing.T) {
	t.Run("Capella BeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBeaconBlockCapella
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionCapella), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Deneb BeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBeaconBlockV(spec.DataVersionDeneb).Deneb.Block
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionDeneb), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Electra BeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBeaconBlockV(spec.DataVersionElectra).Electra.Block
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionElectra), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Capella BlindedBeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBlindedBeaconBlockCapella
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionCapella), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Deneb BlindedBeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBlindedBeaconBlockDeneb
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionDeneb), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Electra BlindedBeaconBlock", func(t *testing.T) {
		block := testingutils.TestingBlindedBeaconBlockElectra
		result, err := ConvertBlockToBeaconBlockData(block)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(spec.DataVersionElectra), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Unknown block type", func(t *testing.T) {
		result, err := ConvertBlockToBeaconBlockData(&phase0.Attestation{})

		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "obj type is unknown")
	})
}
