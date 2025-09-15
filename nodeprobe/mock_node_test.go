package nodeprobe

import (
	"context"
	"fmt"
	"sync/atomic"
)

type nodeMock struct {
	healthy atomic.Pointer[error]
}

func (m *nodeMock) Healthy(context.Context) error {
	err := m.healthy.Load()
	if err != nil {
		return *err
	}
	return nil
}

type stuckNodeMock struct{}

func (m *stuckNodeMock) Healthy(ctx context.Context) error {
	<-ctx.Done() // stuck until the call is canceled
	return ctx.Err()
}

type glitchyNodeMock struct {
	calledCnt atomic.Uint64
}

func (m *glitchyNodeMock) Healthy(ctx context.Context) error {
	if m.calledCnt.Load() >= 2 {
		return nil
	}
	m.calledCnt.Add(1)
	return fmt.Errorf("got a glitch")
}
