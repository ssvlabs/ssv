package duties

import (
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/eth2-key-manager/core"

	"github.com/bloxapp/ssv/operator/duties/mocks"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func TestDutyFetcher_GetDuties(t *testing.T) {
	logger := zap.L()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("handles error", func(t *testing.T) {
		expectedErr := errors.New("test duties")
		mockClient := createBeaconDutiesClient(ctrl, nil, expectedErr)
		mockFetcher := createIndexFetcher(ctrl, []phase0.ValidatorIndex{205238})
		dm := newDutyFetcher(logger, mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork, 0))
		duties, err := dm.GetDuties(logger, 893108)
		require.EqualError(t, err, "failed to get duties from beacon: test duties")
		require.Len(t, duties, 0)
	})

	t.Run("serves duties for the given slot", func(t *testing.T) {
		beaconDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: phase0.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, beaconDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []phase0.ValidatorIndex{205238})
		dm := newDutyFetcher(logger, mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork, 0))

		duties, err := dm.GetDuties(logger, 893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("cache duties", func(t *testing.T) {
		fetchedDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: phase0.BLSPubKey{},
			},
			{
				Slot:   893110,
				PubKey: phase0.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, fetchedDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []phase0.ValidatorIndex{205238})
		dm := newDutyFetcher(logger, mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork, 0))
		duties, err := dm.GetDuties(logger, 893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
		// trying to get duty in another slot, same epoch -> cache should be used
		// cleanup beacon client so it won't return duties
		dm.(*dutyFetcher).beaconClient = mockClient
		// get duties, assuming cache will be used
		duties, err = dm.GetDuties(logger, 893110)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("handles no indices", func(t *testing.T) {
		fetchedDuties := []*spectypes.Duty{
			{
				Slot:   893108,
				PubKey: phase0.BLSPubKey{},
			},
		}
		mockClient := createBeaconDutiesClient(ctrl, fetchedDuties, nil)
		mockFetcher := createIndexFetcher(ctrl, []phase0.ValidatorIndex{})
		dm := newDutyFetcher(logger, mockClient, mockFetcher, beacon.NewNetwork(core.PraterNetwork, 0))
		duties, err := dm.GetDuties(logger, 893108)
		require.NoError(t, err)
		require.Len(t, duties, 0)
	})
}

func TestDutyFetcher_AddMissingSlots(t *testing.T) {
	df := dutyFetcher{
		logger:     zap.L(),
		ethNetwork: beacon.NewNetwork(core.PraterNetwork, 0),
	}
	tests := []struct {
		name string
		slot phase0.Slot
	}{
		{"slot from the middle", phase0.Slot(950120)},
		{"second slot in epoch", phase0.Slot(950113)},
		{"first slot in epoch", phase0.Slot(950112)},
		{"last slot in epoch", phase0.Slot(950143)},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			entries := map[phase0.Slot]cacheEntry{}
			entries[test.slot] = cacheEntry{[]spectypes.Duty{}}
			df.addMissingSlots(entries)
			// require.Equal(t, len(entries), 32)
			_, firstExist := entries[phase0.Slot(950112)]
			require.True(t, firstExist)
			_, lastExist := entries[phase0.Slot(950143)]
			require.True(t, lastExist)
		})
	}
}

func createIndexFetcher(ctrl *gomock.Controller, result []phase0.ValidatorIndex) *mocks.MockvalidatorsIndicesFetcher {
	indexFetcher := mocks.NewMockvalidatorsIndicesFetcher(ctrl)
	indexFetcher.EXPECT().GetValidatorsIndices(gomock.Any()).Return(result).Times(1)

	return indexFetcher
}

func createBeaconDutiesClient(ctrl *gomock.Controller, result []*spectypes.Duty, err error) *beacon.MockBeacon {
	client := beacon.NewMockBeacon(ctrl)
	client.EXPECT().GetDuties(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, err).MaxTimes(1)
	client.EXPECT().SubscribeToCommitteeSubnet(gomock.Any()).Return(nil).MaxTimes(1)

	return client
}
