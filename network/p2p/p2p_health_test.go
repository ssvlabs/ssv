package p2pv1

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/discovery"
)

func TestP2PNetwork_Healthy(t *testing.T) {
	const wedgedErr = "discovery wedged"

	tests := []struct {
		name            string
		state           int32
		discoveryFailed bool
		cancelCtx       bool
		disc            discovery.Service
		hostPeers       int // connected-peer count; -1 means no host at all
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
			// No host set at all: peer count reads as zero, so the wedge is fatal
			// (also pins that Healthy survives a nil host).
			name:      "discovery wedged",
			state:     stateReady,
			disc:      staleDiscovery{stale: true},
			hostPeers: -1,
			wantErr:   wedgedErr,
		},
		{
			name:      "discovery wedged with degraded peer set",
			state:     stateReady,
			disc:      staleDiscovery{stale: true},
			hostPeers: discoveryStalePeerFloor - 1,
			wantErr:   wedgedErr,
		},
		{
			// A stale socket with a healthy peer set may just be inbound UDP lost
			// upstream; a restart can't fix that and would drop every live
			// connection, so it must not fail the probe (no restart loop).
			name:      "discovery wedged but peer set healthy",
			state:     stateReady,
			disc:      staleDiscovery{stale: true},
			hostPeers: discoveryStalePeerFloor,
		},
		{
			name:      "ready with live discovery",
			state:     stateReady,
			disc:      staleDiscovery{stale: false},
			hostPeers: -1,
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
			if tt.hostPeers >= 0 {
				var h host.Host = fakeHost{peers: tt.hostPeers}
				n.host.Store(&h)
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

// fakeHost/fakeNet expose just the connected-peer count Healthy consults; the
// embedded nil interfaces are never dereferenced.
type fakeHost struct {
	host.Host
	peers int
}

func (h fakeHost) Network() p2pnet.Network { return fakeNet{peers: h.peers} }

type fakeNet struct {
	p2pnet.Network
	peers int
}

func (n fakeNet) Peers() []peer.ID { return make([]peer.ID, n.peers) }
