package web3signer

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/internal/beacontest"
	"github.com/ssvlabs/ssv/ssvsigner/internal/ssvtestingutils"
)

func TestConvertBlockToBeaconBlockData(t *testing.T) {
	t.Run("Capella BeaconBlock", func(t *testing.T) {
		dataVersion := spec.DataVersionCapella
		block := ssvtestingutils.TestingBeaconBlockV(dataVersion).Capella
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

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
		dataVersion := spec.DataVersionDeneb
		block := ssvtestingutils.TestingBeaconBlockV(dataVersion).Deneb.Block
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

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
		dataVersion := spec.DataVersionElectra
		block := ssvtestingutils.TestingBeaconBlockV(dataVersion).Electra.Block
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Fulu BeaconBlock", func(t *testing.T) {
		dataVersion := spec.DataVersionFulu
		block := ssvtestingutils.TestingBeaconBlockV(dataVersion).Fulu.Block
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

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
		dataVersion := spec.DataVersionCapella
		block := ssvtestingutils.TestingBlindedBeaconBlockV(dataVersion).CapellaBlinded
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

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
		dataVersion := spec.DataVersionDeneb
		block := ssvtestingutils.TestingBlindedBeaconBlockV(dataVersion).DenebBlinded
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

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
		dataVersion := spec.DataVersionElectra
		block := ssvtestingutils.TestingBlindedBeaconBlockV(dataVersion).ElectraBlinded
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Fulu BlindedBeaconBlock", func(t *testing.T) {
		dataVersion := spec.DataVersionFulu
		block := ssvtestingutils.TestingBlindedBeaconBlockV(dataVersion).FuluBlinded
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))
	})

	t.Run("Gloas BeaconBlock", func(t *testing.T) {
		dataVersion := spec.DataVersionGloas
		block := beacontest.GloasBlock(123, 1)
		result, err := ConvertBlockToBeaconBlockData(block, dataVersion)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, DataVersion(dataVersion), result.Version)

		// Compare header fields
		require.Equal(t, block.Slot, result.BlockHeader.Slot)
		require.Equal(t, block.ProposerIndex, result.BlockHeader.ProposerIndex)
		require.Equal(t, block.ParentRoot, result.BlockHeader.ParentRoot)
		require.Equal(t, block.StateRoot, result.BlockHeader.StateRoot)

		expectedBodyRoot, err := block.Body.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedBodyRoot, [32]byte(result.BlockHeader.BodyRoot))

		// BLOCK_V2 sends only the header, so signing it is sound only if its root is the block's.
		blockRoot, err := block.HashTreeRoot()
		require.NoError(t, err)
		headerRoot, err := result.BlockHeader.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, blockRoot, headerRoot)

		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"version":"GLOAS"`)
	})

	t.Run("Unknown block type", func(t *testing.T) {
		result, err := ConvertBlockToBeaconBlockData(&phase0.Attestation{}, spec.DataVersionUnknown)

		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "obj type is unknown")
	})
}
