package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestValidatorRegistrationRunner_ExecuteDutyDeprecatedFromGloas pins the runner-side SIP #94 §5
// guard (matching ssv-spec's): executing a validator-registration duty at a Gloas-era slot fails
// with ValidatorRegistrationDeprecatedErrorCode before touching the beacon node — from the fork on,
// fee recipient and gas limit travel in the proposer preferences instead. The scheduler drains the
// duty and message validation rejects it on the wire; this is the runner-side belt.
func TestValidatorRegistrationRunner_ExecuteDutyDeprecatedFromGloas(t *testing.T) {
	cfg := cloneTestNetworkConfig()
	gloasEpoch := phase0.Epoch(100)
	cfg.Beacon.Forks[networkconfig.DataVersionGloas] = phase0.Fork{Epoch: gloasEpoch}
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * cfg.SlotsPerEpoch)

	r := &ValidatorRegistrationRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleValidatorRegistration,
			NetworkConfig:  cfg,
		},
	}
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleValidatorRegistration, Slot: gloasSlot}
	r.State = NewRunnerState(1, duty)

	err := r.executeDuty(context.Background(), zap.NewNop(), duty)
	require.Error(t, err)
	var specErr *spectypes.Error
	require.True(t, errors.As(err, &specErr))
	require.Equal(t, spectypes.ValidatorRegistrationDeprecatedErrorCode, specErr.Code)
}

// TestVRSubmitter_StartStopsOnCtxCancel pins the constructor/Start split: NewVRSubmitter returns
// without launching anything (no ctx param, no goroutine), and Start runs the submission loop until
// ctx is canceled. That stop-on-cancel contract is what lets the operator node spawn Start as a
// supervised service and await it cleanly at shutdown.
//
// beacon and validatorStore are nil: with a realistic slot duration no tick fires within the test
// window, so the loop blocks on ctx and never dereferences them.
func TestVRSubmitter_StartStopsOnCtxCancel(t *testing.T) {
	s := NewVRSubmitter(zap.NewNop(), networkconfig.TestNetwork.Beacon, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after ctx cancellation")
	}
}
