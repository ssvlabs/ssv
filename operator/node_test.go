package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	mockslotticker "github.com/ssvlabs/ssv/operator/slotticker/mocks"
	"github.com/ssvlabs/ssv/operator/validator"
	registrymocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

// TestNew_ExporterMode_SchedulerWiring verifies that operator.New() completes successfully in exporter
// mode and wires the duty scheduler with the AllShares provider path (no fee-recipient controller).
// This covers the restructured scheduler-wiring lines that were previously gated by shouldRunDutyScheduler.
func TestNew_ExporterMode_SchedulerWiring(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	mockVS := registrymocks.NewMockValidatorStore(ctrl)
	mockVS.EXPECT().WithOperatorID(gomock.Any()).Return(nil)

	mockTicker := mockslotticker.NewMockSlotTicker(ctrl)

	opts := Options{
		NetworkConfig: networkconfig.TestNetwork,
		Context:       context.Background(),
		ValidatorStore: mockVS,
		ValidatorController: new(validator.Controller),
		ValidatorOptions: validator.ControllerOptions{
			OperatorDataStore: operatordatastore.New(nil),
		},
		DutyStore: dutystore.New(),
	}

	node := New(
		zap.NewNop(),
		opts,
		exporter.Options{Enabled: true},
		func() slotticker.SlotTicker { return mockTicker },
		nil,
	)
	require.NotNil(t, node)
	require.NotNil(t, node.dutyScheduler)
}
