package duties

import (
	"errors"
	"testing"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/operator/duties/mocks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

func TestDutyFetcher_GetDuties(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("handles error", func(t *testing.T) {
		expectedErr := errors.New("test duties")
		mockClient := createBeaconDutiesClient(ctrl, nil, expectedErr)
		mockFetcher := createIndexFetcher(ctrl, []spec.ValidatorIndex{205238})
		dm := newDutyFetcher(zap.L(), mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork))
		duties, err := dm.GetDuties(893108)
		require.EqualError(t, err, "failed to get duties from beacon: test duties")
		require.Len(t, duties, 0)
	})

	t.Run("serves duties for the given slot", func(t *testing.T) {
		beaconDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, beaconDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []spec.ValidatorIndex{205238})
		dm := newDutyFetcher(zap.L(), mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork))

		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("cache duties", func(t *testing.T) {
		fetchedDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
			{
				Slot:   893110,
				PubKey: spec.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, fetchedDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []spec.ValidatorIndex{205238})
		dm := newDutyFetcher(zap.L(), mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
		// trying to get duty in another slot, same epoch -> cache should be used
		// cleanup beacon client so it won't return duties
		dm.(*dutyFetcher).beaconClient = mockClient
		// get duties, assuming cache will be used
		duties, err = dm.GetDuties(893110)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("handles no indices", func(t *testing.T) {
		fetchedDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, fetchedDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []spec.ValidatorIndex{})
		dm := newDutyFetcher(zap.L(), mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 0)
	})
}

func TestDutyFetcher_AddMissingSlots(t *testing.T) {
	df := dutyFetcher{
		logger:     zap.L(),
		ethNetwork: beacon.NewNetwork(core.PraterNetwork),
	}
	tests := []struct {
		name string
		slot spec.Slot
	}{
		{"slot from the middle", spec.Slot(950120)},
		{"second slot in epoch", spec.Slot(950113)},
		{"first slot in epoch", spec.Slot(950112)},
		{"last slot in epoch", spec.Slot(950143)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := map[spec.Slot]cacheEntry{}
			entries[test.slot] = cacheEntry{[]spectypes.Duty{}}
			df.addMissingSlots(entries)
			// require.Equal(t, len(entries), 32)
			_, firstExist := entries[spec.Slot(950112)]
			require.True(t, firstExist)
			_, lastExist := entries[spec.Slot(950143)]
			require.True(t, lastExist)
		})
	}
}

func createIndexFetcher(ctrl *gomock.Controller, result []spec.ValidatorIndex) *mocks.MockvalidatorsIndicesFetcher {
	indexFetcher := mocks.NewMockvalidatorsIndicesFetcher(ctrl)
	indexFetcher.EXPECT().GetValidatorsIndices().Return(result).Times(1)

	return indexFetcher
}

func createBeaconDutiesClient(ctrl *gomock.Controller, result []*spectypes.Duty, err error) *mocks.MockbeaconDutiesClient {
	client := mocks.NewMockbeaconDutiesClient(ctrl)
	client.EXPECT().GetDuties(gomock.Any(), gomock.Any()).Return(result, err).MaxTimes(1)
	client.EXPECT().SubscribeToCommitteeSubnet(gomock.Any()).Return(nil).MaxTimes(1)

	return client
}
