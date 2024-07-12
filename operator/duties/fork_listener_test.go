package duties

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
)

func TestScheduler_ForkListener_Fork_Epoch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pkHex := "8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"
	ks := spectestingutils.Testing4SharesSet()
	ln, _, err := p2pv1.CreateNetworkAndSubscribeFromKeySet(t, ctx, p2pv1.LocalNetOptions{
		Nodes:        4,
		MinConnected: 4/2 - 1,
		UseDiscv5:    false,
	}, ks, pkHex)
	require.NoError(t, err)
	var (
		handler     = NewForkListener(ln.Nodes[0])
		currentSlot = &SafeValue[phase0.Slot]{}
		forkEpoch   = phase0.Epoch(0)
	)
	currentSlot.Set(phase0.Slot(31))
	_, _, mockTicker, _, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot, forkEpoch)
	startFn()
	// STEP 3: wait for attester duties to be executed
	currentSlot.Set(phase0.Slot(15))
	mockTicker.Send(currentSlot.Get())

	currentSlot.Set(phase0.Slot(32))

	require.NoError(t, schedulerPool.Wait())
	// Stop scheduler & wait for graceful exit.
	// cancel()

}
