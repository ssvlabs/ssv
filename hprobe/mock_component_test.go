package hprobe

import (
	"context"
	"fmt"
	"sync/atomic"
)

type componentMock struct {
	healthy atomic.Pointer[error]
}

func (m *componentMock) Healthy(context.Context) error {
	err := m.healthy.Load()
	if err != nil {
		return *err
	}
	return nil
}

type stuckComponentMock struct{}

func (m *stuckComponentMock) Healthy(ctx context.Context) error {
	<-ctx.Done() // stuck until the call is canceled
	return ctx.Err()
}

// ctxIgnoringComponentMock simulates a buggy component whose Healthy ignores its ctx entirely
// (stuck mutex, broken client): it never returns, leaking its probe goroutine by design.
type ctxIgnoringComponentMock struct{}

func (m *ctxIgnoringComponentMock) Healthy(context.Context) error {
	select {} // blocks forever
}

type glitchyComponentMock struct {
	calledCnt        atomic.Uint64
	glitchedCallsCnt uint64
}

func newGlitchyComponentMock(glitchedCallsCnt uint64) *glitchyComponentMock {
	return &glitchyComponentMock{
		glitchedCallsCnt: glitchedCallsCnt,
	}
}

func (m *glitchyComponentMock) Healthy(ctx context.Context) error {
	m.calledCnt.Add(1)
	if m.calledCnt.Load() > m.glitchedCallsCnt {
		return nil
	}
	return fmt.Errorf("got a glitch")
}
