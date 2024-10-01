package validator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestStopCommittee(t *testing.T) {
	t.Run("initial state is running", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cmt := NewCommittee(ctx, cancel, zap.NewNop(), "BeaconNetwork", nil, nil, nil)
		assert.False(t, cmt.Stopped())
	})

	t.Run("stop changes state and cancels context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cmt := NewCommittee(ctx, cancel, zap.NewNop(), "BeaconNetwork", nil, nil, nil)

		cmt.stop()

		assert.True(t, cmt.Stopped())
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	})
}
