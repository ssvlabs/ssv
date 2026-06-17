package hprobe

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
		clComponentName          = "clComponentName"
		elComponentName          = "elComponentName"
		eventSyncerComponentName = "eventSyncerComponentName"
	)

	ctx := t.Context()

	t.Run("1 component, success", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		err := p.Probe(ctx, clComponentName)
		require.NoError(t, err)
	})

	t.Run("1 component, success with retry delay", func(t *testing.T) {
		glitchesCnt := 3
		clComponent := newGlitchyComponentMock(uint64(glitchesCnt))

		retryDelay := 100 * time.Millisecond

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, retryDelay)

		startTime := time.Now()
		err := p.Probe(ctx, clComponentName)
		took := time.Since(startTime)
		require.NoError(t, err)
		require.True(t, took > time.Duration(glitchesCnt)*retryDelay)
	})

	t.Run("1 component, success with glitchy component", func(t *testing.T) {
		clComponent := newGlitchyComponentMock(2)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		err := p.Probe(ctx, clComponentName)
		require.NoError(t, err)
	})

	t.Run("1 component, probe not found", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		err := p.Probe(ctx, elComponentName)
		require.ErrorContains(t, err, "not found")
		require.ErrorContains(t, err, elComponentName)
	})

	t.Run("1 component, probe failed due to component error", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		clDownErr := fmt.Errorf("some error")
		clComponent.healthy.Store(&clDownErr)

		err := p.Probe(ctx, clComponentName)
		require.ErrorContains(t, err, clDownErr.Error())
		require.ErrorContains(t, err, clComponentName)
	})

	t.Run("1 component, probe failed due to component stuck", func(t *testing.T) {
		clComponent := &stuckComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := p.Probe(probeCtx, clComponentName)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("1 component, probe canceled is not an error", func(t *testing.T) {
		clComponent := &stuckComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithCancel(ctx)
		cancel()
		err := p.Probe(probeCtx, clComponentName)
		require.NoError(t, err)
	})

	t.Run("all components are healthy", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		elComponent := &componentMock{}
		elComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("CL went down", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		elComponent := &componentMock{}
		elComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		clDownErr := fmt.Errorf("some error")
		clComponent.healthy.Store(&clDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, clDownErr.Error())
	})

	t.Run("EL went down", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		elComponent := &componentMock{}
		elComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		elDownErr := fmt.Errorf("some error")
		elComponent.healthy.Store(&elDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, elDownErr.Error())
	})

	t.Run("all components + event-syncer are healthy", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		elComponent := &componentMock{}
		elComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerComponent := &componentMock{}
		eventSyncerComponent.healthy.Store(nil)

		p.AddComponent(eventSyncerComponentName, eventSyncerComponent, 10*time.Second, 5, 0)

		err = p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("event-syncer went down", func(t *testing.T) {
		clComponent := &componentMock{}
		clComponent.healthy.Store(nil)

		elComponent := &componentMock{}
		elComponent.healthy.Store(nil)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerComponent := &componentMock{}
		eventSyncerComponent.healthy.Store(nil)

		p.AddComponent(eventSyncerComponentName, eventSyncerComponent, 10*time.Second, 5, 0)

		err = p.ProbeAll(ctx)
		require.NoError(t, err)

		eventSyncerDownErr := fmt.Errorf("some error")
		eventSyncerComponent.healthy.Store(&eventSyncerDownErr)

		err = p.ProbeAll(ctx)
		require.ErrorContains(t, err, eventSyncerDownErr.Error())
	})

	t.Run("probe deadline hit (timeout configured via AddComponent)", func(t *testing.T) {
		clComponent := &stuckComponentMock{}
		elComponent := &stuckComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Millisecond, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Millisecond, 5, 0)

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
		clComponent := &stuckComponentMock{}
		elComponent := &stuckComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := p.ProbeAll(probeCtx)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("probe context cancel is not an error", func(t *testing.T) {
		clComponent := &stuckComponentMock{}
		elComponent := &stuckComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		probeCtx, cancel := context.WithCancel(ctx)
		cancel()
		err := p.ProbeAll(probeCtx)
		require.NoError(t, err)
	})

	t.Run("wedged component cannot stall the round past its deadline", func(t *testing.T) {
		// A Healthy impl that ignores its ctx never returns; the round must still end at the ctx
		// deadline with a timeout verdict — otherwise a single wedged component would hang every
		// ProbeAll caller forever. The wedged probe goroutine leaks and is reaped at process exit.
		clComponent := &ctxIgnoringComponentMock{}

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Millisecond, 0, 0)

		probeCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		err := p.ProbeAll(probeCtx)
		require.ErrorContains(t, err, "deadline exceeded")
	})

	t.Run("sibling cut short by early-cancel is not part of the verdict", func(t *testing.T) {
		// The CL fails outright; its failure early-cancels the round while the EL is parked in its
		// retry-delay. The EL's cut-short probe must contribute nothing: the verdict carries only the
		// genuine failure and must not be classifiable as a cancellation (#2880).
		clComponent := &componentMock{}
		clDownErr := fmt.Errorf("some error")
		clComponent.healthy.Store(&clDownErr)

		elComponent := newGlitchyComponentMock(1) // first attempt fails -> parks in retry-delay

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 0, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 10*time.Second)

		err := p.ProbeAll(ctx)
		require.ErrorContains(t, err, clComponentName)
		require.ErrorContains(t, err, clDownErr.Error())
		require.NotErrorIs(t, err, context.Canceled)
		require.NotContains(t, err.Error(), elComponentName)
	})

	t.Run("component glitches survived via retries", func(t *testing.T) {
		clComponent := newGlitchyComponentMock(2)

		elComponent := newGlitchyComponentMock(3)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.NoError(t, err)
	})

	t.Run("component glitches exceeding max probe retries", func(t *testing.T) {
		clComponent := newGlitchyComponentMock(2)

		elComponent := newGlitchyComponentMock(6)

		p := NewHealthProber(log.TestLogger(t))
		p.AddComponent(clComponentName, clComponent, 10*time.Second, 5, 0)
		p.AddComponent(elComponentName, elComponent, 10*time.Second, 5, 0)

		err := p.ProbeAll(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, "probe health-check failed: probe component elComponentName: component is unhealthy: got a glitch")
	})
}
