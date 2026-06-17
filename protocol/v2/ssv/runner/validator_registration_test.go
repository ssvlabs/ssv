package runner

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

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
