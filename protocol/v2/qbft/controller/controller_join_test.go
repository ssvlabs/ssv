package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// rejectingValueChecker fails every value. JoinInstance has no value of its own to check, so it must
// never consult the checker at start; the instance keeps it for the leader's proposal.
type rejectingValueChecker struct{}

func (rejectingValueChecker) CheckValue([]byte) error { return errors.New("rejected") }

func TestController_JoinInstance(t *testing.T) {
	h := newForkTestHarness()
	logger := zap.NewNop()

	t.Run("starts a voter instance without a value or a start-time value check", func(t *testing.T) {
		ctrl := h.newController()

		inst, err := ctrl.JoinInstance(context.Background(), logger, h.postForkHeight, rejectingValueChecker{}, h.roundTimerF)
		require.NoError(t, err)
		require.Empty(t, inst.StartValue)
		require.Same(t, inst, ctrl.RecentInstances.FindInstance(h.postForkHeight))
		require.Equal(t, h.postForkHeight, ctrl.LatestInstanceHeight)
		// Same fork-aware identifier derivation as StartNewInstance.
		require.Equal(t, h.cfg.NextDomainType[:], instanceDomain(t, inst.State.ID))
	})

	t.Run("rejects an already-running or past height, like StartNewInstance", func(t *testing.T) {
		ctrl := h.newController()
		_, err := ctrl.JoinInstance(context.Background(), logger, h.postForkHeight, okValueChecker{}, h.roundTimerF)
		require.NoError(t, err)

		_, err = ctrl.JoinInstance(context.Background(), logger, h.postForkHeight, okValueChecker{}, h.roundTimerF)
		require.Error(t, err, "height already running")

		_, err = ctrl.JoinInstance(context.Background(), logger, h.preForkHeight, okValueChecker{}, h.roundTimerF)
		require.Error(t, err, "past height")
	})
}
