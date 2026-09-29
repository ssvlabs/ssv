package runner

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/qa/faults"
)

// oneTickTicker delivers exactly one tick at slot, then closes processed on the loop's next call
// to Next — which the loop makes only after it has fully handled the first tick.
type oneTickTicker struct {
	slot      phase0.Slot
	tick      chan time.Time
	processed chan struct{}
	calls     int
}

func (tk *oneTickTicker) Next() <-chan time.Time {
	tk.calls++
	if tk.calls == 1 {
		return tk.tick
	}
	if tk.calls == 2 {
		close(tk.processed)
	}
	return make(chan time.Time) // never fires
}

func (tk *oneTickTicker) Slot() phase0.Slot { return tk.slot }

type noSelfValidators struct{}

func (noSelfValidators) SelfValidators() []*ssvtypes.SSVShare { return nil }

// runVRSubmitterOneGloasTick enqueues one registration, drives VRSubmitter.start through one tick
// at the first Gloas slot, and returns once that tick is fully processed.
func runVRSubmitterOneGloasTick(t *testing.T, bn beacon.BeaconNode) {
	t.Helper()
	const gloasEpoch = phase0.Epoch(100)
	cfg := networkconfig.TestNetworkWithGloas(gloasEpoch)

	s := NewVRSubmitter(zap.NewNop(), cfg.Beacon, bn, noSelfValidators{})
	require.NoError(t, s.Enqueue(&api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &v1.SignedValidatorRegistration{
			Message: &v1.ValidatorRegistration{Pubkey: phase0.BLSPubKey{1, 2, 3}},
		},
	}))

	tk := &oneTickTicker{
		slot:      cfg.FirstSlotAtEpoch(gloasEpoch),
		tick:      make(chan time.Time, 1),
		processed: make(chan struct{}),
	}
	tk.tick <- time.Now()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.start(ctx, tk)
		close(done)
	}()

	select {
	case <-tk.processed:
	case <-time.After(5 * time.Second):
		t.Fatal("VRSubmitter did not process the tick")
	}
	cancel()
	<-done
}

// TestVRSubmitterSkipsGloasUnlessFault pins the fourth Gloas gate (errata #137's one-line fix):
// VRSubmitter.start submits nothing to the beacon node at a Gloas slot, unless vr-postfork is set.
// This gate is DOWNSTREAM of the wire — it only drains registrations reconstructed after a
// pre-consensus quorum — so it never held vr-postfork's injections at 0; it is aligned with the
// other three gates for consistency, not as the #137 root cause.
func TestVRSubmitterSkipsGloasUnlessFault(t *testing.T) {
	t.Run("honest node submits nothing at a Gloas slot", func(t *testing.T) {
		bn := beacon.NewMockBeaconNode(gomock.NewController(t))
		// No EXPECT: any SubmitValidatorRegistrations call fails the test.
		runVRSubmitterOneGloasTick(t, bn)
	})

	t.Run("vr-postfork keeps submitting at a Gloas slot", func(t *testing.T) {
		faults.SetForTest(t, faults.VRPostFork)
		bn := beacon.NewMockBeaconNode(gomock.NewController(t))
		bn.EXPECT().SubmitValidatorRegistrations(gomock.Any(), gomock.Len(1)).Return(nil).Times(1)
		runVRSubmitterOneGloasTick(t, bn)
	})
}
