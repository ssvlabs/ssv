package p2pv1

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/discovery"
)

func TestP2PNetwork_Healthy(t *testing.T) {
	tests := []struct {
		name            string
		state           int32
		discoveryFailed bool
		cancelCtx       bool
		disc            discovery.Service
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
		{
			name:    "discovery wedged",
			state:   stateReady,
			disc:    staleDiscovery{stale: true},
			wantErr: "discovery wedged",
		},
		{
			name:  "ready with live discovery",
			state: stateReady,
			disc:  staleDiscovery{stale: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &p2pNetwork{logger: zap.NewNop()}
			n.disc = tt.disc
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

// staleDiscovery is a discovery.Service whose only real method is DiscoveryStale;
// Healthy calls nothing else, so the embedded nil Service is never dereferenced.
type staleDiscovery struct {
	discovery.Service
	stale bool
}

func (d staleDiscovery) DiscoveryStale(time.Duration) bool { return d.stale }
