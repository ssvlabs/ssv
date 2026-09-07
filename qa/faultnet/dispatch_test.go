package faultnet

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/qa/faults"
)

// fakeP2P is a minimal network.P2PNetwork: it records every message handed to BroadcastAtSlot and
// leaves every other method to the embedded nil interface, which this suite never calls.
type fakeP2P struct {
	network.P2PNetwork

	mu   sync.Mutex
	sent []*spectypes.SignedSSVMessage
}

func (f *fakeP2P) BroadcastAtSlot(msg *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeP2P) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeP2P) at(i int) *spectypes.SignedSSVMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[i]
}

// fakeSigner is a minimal ssvtypes.OperatorSigner. failAt, if non-zero, is the 1-based call index
// on which SignSSVMessage fails; every other call succeeds.
type fakeSigner struct {
	mu     sync.Mutex
	calls  int
	failAt int
}

func (f *fakeSigner) SignSSVMessage(*spectypes.SSVMessage) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if f.failAt != 0 && n == f.failAt {
		return nil, errors.New("fake signer: forced failure")
	}
	return []byte{0xaa}, nil
}

func (f *fakeSigner) GetOperatorID() spectypes.OperatorID { return 7 }

func (f *fakeSigner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newTestNetwork wires a Network to fakeP2P/fakeSigner and an observed logger, so a test can dispatch
// Outgoings and then assert on both what reached the fake wire and what was logged.
func newTestNetwork(signer *fakeSigner) (*Network, *fakeP2P, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	inner := &fakeP2P{}
	n := &Network{P2PNetwork: inner, signer: signer, logger: zap.New(core)}
	return n, inner, logs
}

const fireSnippet = "qa fault injected"
const summarySnippet = "repeated send series finished"

func TestDispatchSingleResignedSendAnnouncesOnce(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	n, inner, logs := newTestNetwork(&fakeSigner{})
	msg := partialMsg(t, spectypes.RoleProposerPreferences, 100)

	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 100, Resign: true}})

	require.NoError(t, err)
	require.Equal(t, 1, inner.count(), "the message must reach the inner network exactly once")
	require.Equal(t, 1, logs.FilterMessageSnippet(fireSnippet).Len(), "a single resigned send announces exactly once")
	require.Equal(t, 0, logs.FilterMessageSnippet(summarySnippet).Len(), "Repeat == 0: no summary line")
}

func TestDispatchIdentitySendAnnouncesNothing(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	n, inner, logs := newTestNetwork(&fakeSigner{})
	msg := partialMsg(t, spectypes.RoleProposerPreferences, 100)
	originalData := append([]byte(nil), msg.SSVMessage.Data...)

	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 100, Resign: false}})

	require.NoError(t, err)
	require.Equal(t, 1, inner.count())
	require.Same(t, msg, inner.at(0), "the identity path must forward the original pointer, not a copy")
	require.Equal(t, originalData, inner.at(0).SSVMessage.Data, "the identity path must not mutate the bytes")
	require.Equal(t, 0, logs.FilterMessageSnippet(fireSnippet).Len(), "an unsigned identity send announces nothing")
	require.Equal(t, 0, logs.FilterMessageSnippet(summarySnippet).Len())
}

func TestDispatchRepeatedSendAnnouncesOnceAndSummarizes(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	n, inner, logs := newTestNetwork(&fakeSigner{})
	msg := partialMsg(t, spectypes.RoleProposerPreferences, 100)

	// A small shape, built directly rather than through Plan/prefsReplay — real replay constants
	// would make this test run for 13 minutes.
	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 100, Resign: true, Repeat: 2, Every: time.Millisecond}})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return inner.count() == 3
	}, time.Second, 5*time.Millisecond, "the first send plus 2 repeats must all reach the wire")

	require.Eventually(t, func() bool {
		return logs.FilterMessageSnippet(summarySnippet).Len() == 1
	}, time.Second, 5*time.Millisecond, "exactly one summary line must close the series")

	require.Equal(t, 1, logs.FilterMessageSnippet(fireSnippet).Len(), "only the first send announces; the other 2 stay silent")

	summary := logs.FilterMessageSnippet(summarySnippet).All()[0].ContextMap()
	require.EqualValues(t, 3, summary["sent"], "the summary must report how many actually went out")
	require.EqualValues(t, 3, summary["planned"], "planned is Repeat+1")
}

func TestDispatchStopsSeriesWhenSigningFails(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	signer := &fakeSigner{failAt: 1}
	n, inner, logs := newTestNetwork(signer)
	msg := partialMsg(t, spectypes.RoleProposerPreferences, 100)

	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 100, Resign: true, Repeat: 2, Every: time.Millisecond}})
	require.NoError(t, err, "dispatch itself never returns the async series' error")

	require.Eventually(t, func() bool {
		return logs.FilterMessageSnippet(summarySnippet).Len() == 1
	}, time.Second, 5*time.Millisecond, "the series still closes out its summary line")

	require.Equal(t, 0, inner.count(), "no message reaches the wire once signing fails on the first attempt")
	require.Equal(t, 0, logs.FilterMessageSnippet(fireSnippet).Len(), "a failed sign must never announce")
	require.Equal(t, 1, signer.callCount(), "the series must stop after the first failure, not retry the remaining repeats")

	summary := logs.FilterMessageSnippet(summarySnippet).All()[0].ContextMap()
	require.EqualValues(t, 0, summary["sent"])
	require.EqualValues(t, 3, summary["planned"])
}
