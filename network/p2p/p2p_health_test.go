package p2pv1

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP2PNetwork_Healthy(t *testing.T) {
	tests := []struct {
		name            string
		state           int32
		discoveryFailed bool
		cancelCtx       bool
		wantErr         string
	}{
		{
			name:    "not ready",
			state:   stateClosed,
			wantErr: "p2p network not ready",
		},
		{
			name:  "ready and healthy",
			state: stateReady,
		},
		{
			name:            "discovery failed",
			state:           stateReady,
			discoveryFailed: true,
			wantErr:         "discovery bootstrap failed",
		},
		{
			name:            "not ready takes precedence over discovery failed",
			state:           stateClosed,
			discoveryFailed: true,
			wantErr:         "p2p network not ready",
		},
		{
			name:      "canceled context",
			state:     stateReady,
			cancelCtx: true,
			wantErr:   "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &p2pNetwork{}
			atomic.StoreInt32(&n.state, tt.state)
			if tt.discoveryFailed {
				n.discoveryFailed.Store(true)
			}

			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			err := n.Healthy(ctx)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
