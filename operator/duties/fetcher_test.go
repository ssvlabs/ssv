package duties

import (
	"errors"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestDutyFetcher_GetDuties(t *testing.T) {
	t.Run("handles error", func(t *testing.T) {
		expectedErr := errors.New("test duties")
		bcMock := beaconDutiesClientMock{
			getDutiesErr: expectedErr,
		}
		dm := newDutyFetcher(zap.L(), &bcMock, &indicesFetcher{[]spec.ValidatorIndex{205238}}, core.PraterNetwork)
		duties, err := dm.GetDuties(893108)
		require.EqualError(t, err, "failed to get duties from beacon: test duties")
		require.Len(t, duties, 0)
	})

	t.Run("serves duties for the given slot", func(t *testing.T) {
		beaconDuties := []*beacon.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		bcMock := beaconDutiesClientMock{duties: beaconDuties}
		dm := newDutyFetcher(zap.L(), &bcMock, &indicesFetcher{[]spec.ValidatorIndex{205238}},
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("cache duties", func(t *testing.T) {
		fetchedDuties := []*beacon.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
			{
				Slot:   893110,
				PubKey: spec.BLSPubKey{},
			},
		}
		bcMock := beaconDutiesClientMock{duties: fetchedDuties}
		dm := newDutyFetcher(zap.L(), &bcMock, &indicesFetcher{[]spec.ValidatorIndex{205238}},
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
		// trying to get duty in another slot, same epoch -> cache should be used
		// cleanup beacon client so it won't return duties
		dm.(*dutyFetcher).beaconClient = &beaconDutiesClientMock{}
		// get duties, assuming cache will be used
		duties, err = dm.GetDuties(893110)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("handles no indices", func(t *testing.T) {
		fetchedDuties := []*beacon.Duty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		bcMock := beaconDutiesClientMock{duties: fetchedDuties}
		dm := newDutyFetcher(zap.L(), &bcMock, &indicesFetcher{[]spec.ValidatorIndex{}},
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 0)
	})
}

type indicesFetcher struct {
	vIndices []spec.ValidatorIndex
}

func (f *indicesFetcher) GetValidatorsIndices() []spec.ValidatorIndex {
	return f.vIndices[:]
}

type beaconDutiesClientMock struct {
	duties            []*beacon.Duty
	getDutiesErr      error
	subToCommitteeErr error
	subscribed        bool
}

// GetDuties returns duties for the passed validators indices
func (bc *beaconDutiesClientMock) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*beacon.Duty, error) {
	return bc.duties, bc.getDutiesErr
}

// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
func (bc *beaconDutiesClientMock) SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	bc.subscribed = true
	return bc.subToCommitteeErr
}
