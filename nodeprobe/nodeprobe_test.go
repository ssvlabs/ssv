package nodeprobe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

func TestProber(t *testing.T) {
	const (
		clNodeName          = "clNodeName"
		elNodeName          = "elNodeName"
		eventSyncerNodeName = "eventSyncerNodeName"
	)

	ctx := t.Context()

	t.Run("1 node, success", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)

		err := p.Probe(ctx, clNodeName)
		require.NoError(t, err)
	})

	t.Run("1 node, success with retry delay", func(t *testing.T) {
		glitchesCnt := 3
		clNode := newGlitchyNodeMock(uint64(glitchesCnt))

		retryDelay := 100 * time.Millisecond

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, retryDelay)

		startTime := time.Now()
		err := p.Probe(ctx, clNodeName)
		took := time.Since(startTime)
		require.NoError(t, err)
		require.True(t, took > time.Duration(glitchesCnt)*retryDelay)
	})

	t.Run("1 node, success with glitchy node", func(t *testing.T) {
		clNode := newGlitchyNodeMock(2)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)

		err := p.Probe(ctx, clNodeName)
		require.NoError(t, err)
	})

	t.Run("1 node, probe not found", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)

		err := p.Probe(ctx, elNodeName)
		require.ErrorContains(t, err, "not found")
		require.ErrorContains(t, err, elNodeName)
	})

	t.Run("1 node, probe failed due to node error", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)

		clDownErr := fmt.Errorf("some error")
		clNode.healthy.Store(&clDownErr)

		err := p.Probe(ctx, clNodeName)
		require.ErrorContains(t, err, clDownErr.Error())
		require.ErrorContains(t, err, clNodeName)
	})

	t.Run("1 node, probe failed due to node stuck", func(t *testing.T) {
		clNode := &stuckNodeMock{}

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := p.Probe(probeCtx, clNodeName)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("all nodes are healthy", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("CL went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		clDownErr := fmt.Errorf("some error")
		clNode.healthy.Store(&clDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, clDownErr.Error())
	})

	t.Run("EL went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		elDownErr := fmt.Errorf("some error")
		elNode.healthy.Store(&elDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, elDownErr.Error())
	})

	t.Run("all nodes + event-syncer are healthy", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerNode := &nodeMock{}
		eventSyncerNode.healthy.Store(nil)

		p.AddNode(eventSyncerNodeName, eventSyncerNode, 10*time.Second, 5, 0)

		err = p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("event-syncer went down", func(t *testing.T) {
		clNode := &nodeMock{}
		clNode.healthy.Store(nil)

		elNode := &nodeMock{}
		elNode.healthy.Store(nil)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerNode := &nodeMock{}
		eventSyncerNode.healthy.Store(nil)

		p.AddNode(eventSyncerNodeName, eventSyncerNode, 10*time.Second, 5, 0)

		err = p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerDownErr := fmt.Errorf("some error")
		eventSyncerNode.healthy.Store(&eventSyncerDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, eventSyncerDownErr.Error())
	})

	t.Run("probe deadline hit (timeout configured via AddNode)", func(t *testing.T) {
		clNode := &stuckNodeMock{}
		elNode := &stuckNodeMock{}

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Millisecond, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Millisecond, 5, 0)

		errCh := make(chan error)
		go func() {
			probeCtx := t.Context()
			errCh <- p.ProbeAll(probeCtx)
		}()

		select {
		case err := <-errCh:
			require.ErrorContains(t, err, "deadline exceeded")
		case <-time.After(5 * time.Second):
			require.Fail(t, "test timed out")
		}
	})

	t.Run("probe deadline hit (parent context deadlined)", func(t *testing.T) {
		clNode := &stuckNodeMock{}
		elNode := &stuckNodeMock{}

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := p.ProbeAll(probeCtx)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("probe context cancel is not an error", func(t *testing.T) {
		clNode := &stuckNodeMock{}
		elNode := &stuckNodeMock{}

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithCancel(ctx)
		cancel()
		err := p.ProbeAll(probeCtx)
		require.NoError(t, err)
	})

	t.Run("node glitches survived via retries", func(t *testing.T) {
		clNode := newGlitchyNodeMock(2)

		elNode := newGlitchyNodeMock(3)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("node glitches exceeding max probe retries", func(t *testing.T) {
		clNode := newGlitchyNodeMock(2)

		elNode := newGlitchyNodeMock(6)

		p := New(log.TestLogger(t))
		p.AddNode(clNodeName, clNode, 10*time.Second, 5, 0)
		p.AddNode(elNodeName, elNode, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, "probe health-check failed: probe node elNodeName: node is unhealthy: got a glitch")
	})
}
