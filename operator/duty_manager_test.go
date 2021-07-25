package operator

import (
	"errors"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestDutyManager_GetDuties(t *testing.T) {
	vIndices = append(vIndices, 205238)

	t.Run("handles error", func(t *testing.T) {
		expectedErr := errors.New("test duties")
		bcMock := beaconDutiesClientMock{
			getDutiesErr: expectedErr,
		}
		dm := NewDutyManager(zap.L(), &bcMock, getValidatorsIndicesMock,
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.EqualError(t, err, "failed to get attest duties: test duties")
		require.Len(t, duties, 0)
	})

	t.Run("serves duties for the given slot", func(t *testing.T) {
		attesterDuties := []*eth2apiv1.AttesterDuty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		bcMock := beaconDutiesClientMock{duties: attesterDuties}
		dm := NewDutyManager(zap.L(), &bcMock, getValidatorsIndicesMock,
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("cache duties", func(t *testing.T) {
		attesterDuties := []*eth2apiv1.AttesterDuty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
			{
				Slot:   893110,
				PubKey: spec.BLSPubKey{},
			},
		}
		bcMock := beaconDutiesClientMock{duties: attesterDuties}
		dm := NewDutyManager(zap.L(), &bcMock, getValidatorsIndicesMock,
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		require.NoError(t, err)
		require.Len(t, duties, 1)
		// trying to get duty in another slot, same epoch -> cache should be used
		// cleanup beacon client so it won't return duties
		dm.(*dutyManager).beaconClient = &beaconDutiesClientMock{}
		// get duties, assuming cache will be used
		duties, err = dm.GetDuties(893110)
		require.NoError(t, err)
		require.Len(t, duties, 1)
	})

	t.Run("handles no indices", func(t *testing.T) {
		attesterDuties := []*eth2apiv1.AttesterDuty{
			{
				Slot:   893108,
				PubKey: spec.BLSPubKey{},
			},
		}
		vIndicesOrig := vIndices[:]
		vIndices = []spec.ValidatorIndex{}
		bcMock := beaconDutiesClientMock{duties: attesterDuties}
		dm := NewDutyManager(zap.L(), &bcMock, getValidatorsIndicesMock,
			core.NetworkFromString(string(core.PraterNetwork)))
		duties, err := dm.GetDuties(893108)
		vIndices = vIndicesOrig[:]
		require.NoError(t, err)
		require.Len(t, duties, 0)
	})
}

var vIndices []spec.ValidatorIndex

func getValidatorsIndicesMock() []spec.ValidatorIndex {
	return vIndices[:]
}

type beaconDutiesClientMock struct {
	duties            []*eth2apiv1.AttesterDuty
	getDutiesErr      error
	subToCommitteeErr error
	subscribed        bool
}

// GetDuties returns duties for the passed validators indices
func (bc *beaconDutiesClientMock) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	return bc.duties, bc.getDutiesErr
}

// SubscribeToCommitteeSubnet subscribe committee to subnet (p2p topic)
func (bc *beaconDutiesClientMock) SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	bc.subscribed = true
	return bc.subToCommitteeErr
}
