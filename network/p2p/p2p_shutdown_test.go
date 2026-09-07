package p2pv1

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

const timeout = 400 * time.Millisecond

type mockTopicsController struct {
	updateCalled     chan struct{}
	deregisterCalled chan struct{}
}

func (f *mockTopicsController) Subscribe(string) error {
	return nil
}

func (f *mockTopicsController) Unsubscribe(string, bool) error {
	return nil
}

func (f *mockTopicsController) Peers(string) ([]peer.ID, error) {
	return nil, nil
}

func (f *mockTopicsController) Topics() []string {
	return nil
}

func (f *mockTopicsController) Broadcast(string, []byte, time.Duration) error {
	return nil
}

func (f *mockTopicsController) DeregisterTopics(...string) {
	select {
	case f.deregisterCalled <- struct{}{}:
	default:
	}
}

func (f *mockTopicsController) UpdateScoreParams() error {
	select {
	case f.updateCalled <- struct{}{}:
	default:
	}
	return nil
}

func (f *mockTopicsController) Close() error {
	return nil
}

func TestUpdateSubnetsStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	// A post-fork config (Boole at genesis) makes the first loop iteration run the
	// subscription-filter tightening path under both CI fork configs. That path dereferences cfg
	// (nil here previously, which flaked this test as a panic) and calls DeregisterTopics, which
	// signals that the loop is running so we cancel it mid-run instead of racing startup.
	//
	// idx and disc are left nil deliberately: empty persistent/committee subnets keep
	// currentSubnets empty, so the subnet-changes branch is never entered. Adding a committee to
	// this fixture would reach it and panic.
	topicsCtrl := &mockTopicsController{deregisterCalled: make(chan struct{}, 1)}

	n := &p2pNetwork{
		ctx:                  ctx,
		logger:               zap.NewNop(),
		cfg:                  &Config{NetworkConfig: testNetworkWithBoole(0)},
		topicsCtrl:           topicsCtrl,
		subscribedCommittees: hashmap.New[string, statusWithSubnet](),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.UpdateSubnets()
	}()

	select {
	case <-topicsCtrl.deregisterCalled:
	case <-time.After(timeout):
		// The signal is the Alan-whitelist tightening in UpdateSubnets; if that block is removed
		// post-Boole, pick a new signal here rather than treating this as a shutdown regression.
		require.Fail(t, "UpdateSubnets did not run its initial iteration")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(timeout):
		require.Fail(t, "UpdateSubnets did not stop after context cancellation")
	}
}

func TestUpdateScoreParamsStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	topicsCtrl := &mockTopicsController{updateCalled: make(chan struct{}, 1)}

	n := &p2pNetwork{
		ctx:        ctx,
		logger:     zap.NewNop(),
		cfg:        &Config{NetworkConfig: networkconfig.TestNetwork},
		topicsCtrl: topicsCtrl,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.UpdateScoreParams()
	}()

	select {
	case <-topicsCtrl.updateCalled:
	case <-time.After(timeout):
		require.Fail(t, "UpdateScoreParams did not run its initial update")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(timeout):
		require.Fail(t, "UpdateScoreParams did not stop after context cancellation")
	}
}
