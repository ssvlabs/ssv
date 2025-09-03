package nodeprobe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProber(t *testing.T) {
	ctx := t.Context()

	t.Run("all nodes are healthy", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)
	})

	t.Run("CL went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)

		clDownErr := fmt.Errorf("some error")
		clNode.healthy.Store(&clDownErr)

		err = p.Probe(ctx)
		require.ErrorContains(t, err, clDownErr.Error())
	})

	t.Run("EL went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)

		elDownErr := fmt.Errorf("some error")
		elNode.healthy.Store(&elDownErr)

		err = p.Probe(ctx)
		require.ErrorContains(t, err, elDownErr.Error())
	})

	t.Run("all nodes + event-syncer are healthy", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)

		eventSyncerNode := &nodeMock{}
		eventSyncerNode.healthy.Store(nil)

		p.AddEventSyncer(eventSyncerNode)

		err = p.Probe(ctx)
		require.NoError(t, err)
	})

	t.Run("event-syncer went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)

		eventSyncerNode := &nodeMock{}
		eventSyncerNode.healthy.Store(nil)

		p.AddEventSyncer(eventSyncerNode)

		err = p.Probe(ctx)
		require.NoError(t, err)

		eventSyncerDownErr := fmt.Errorf("some error")
		eventSyncerNode.healthy.Store(&eventSyncerDownErr)

		err = p.Probe(ctx)
		require.ErrorContains(t, err, eventSyncerDownErr.Error())
	})

	t.Run("probe deadline hit", func(t *testing.T) {
		clNode := &stuckNodeMock{}
		clNode.healthy.Store(nil)

		elNode := &stuckNodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := p.Probe(probeCtx)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("probe context cancel is not an error", func(t *testing.T) {
		clNode := &stuckNodeMock{}
		clNode.healthy.Store(nil)

		elNode := &stuckNodeMock{}
		elNode.healthy.Store(nil)

		p := NewProber(zap.L(), clNode, elNode)

		probeCtx, cancel := context.WithCancel(ctx)
		cancel()
		err := p.Probe(probeCtx)
		require.NoError(t, err)
	})

	t.Run("node glitches survived via retries", func(t *testing.T) {
		clNode := &glitchyNodeMock{}

		elNode := &glitchyNodeMock{}

		p := NewProber(zap.L(), clNode, elNode)

		err := p.Probe(ctx)
		require.NoError(t, err)
	})
}
