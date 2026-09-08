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
// leaves every other method to the embedded nil interface, which this suite never calls. failAt, if
// non-zero, is the 1-based call index on which BroadcastAtSlot returns an error instead of
// recording the message — mirroring fakeSigner's failAt below, for FIX 5's dispatch-level tests.
//
// sentData snapshots SSVMessage.Data at the moment of each successful call, independent of `sent`
// (which keeps the live *SignedSSVMessage pointer). A repeated series reuses the same Outgoing.Msg
// pointer across iterations and mutates it in place, so inspecting `sent` after the whole series has
// run would only ever show every entry's FINAL, most-recently-mutated bytes; `sentData` is what lets
// FIX 3's byte-distinctness test see what was actually on the wire at each individual send.
type fakeP2P struct {
	network.P2PNetwork

	mu       sync.Mutex
	sent     []*spectypes.SignedSSVMessage
	sentData [][]byte
	calls    int
	failAt   int
}

func (f *fakeP2P) BroadcastAtSlot(msg *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failAt != 0 && f.calls == f.failAt {
		return errors.New("fake p2p: forced failure")
	}
	f.sent = append(f.sent, msg)
	f.sentData = append(f.sentData, append([]byte(nil), msg.SSVMessage.Data...))
	return nil
}

func (f *fakeP2P) dataAt(i int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sentData[i]
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

// TestDispatchRepeatedSendsAreByteDistinct pins FIX 3's delivery shape: every send in a repeated
// series must reach the wire as a distinct SignedSSVMessage, not the same bytes re-signed. Before
// the fix, sendAsync re-signed byte-identical bytes on every repeat; SignSSVMessage is deterministic,
// so every send after the first would have re-encoded to the exact same message, and gossipsub's own
// dedup (network/topics/msg_id.go) would silently drop every one of them before it left this node —
// a real deployment would publish ~1 message while the summary line claimed replayCount+1. This test
// exercises the real dispatch -> sendAsync -> send path (not just Plan, which never sees a repeat
// iteration) with a small, fast Repeat count standing in for prefs-replay's real ~15,840.
func TestDispatchRepeatedSendsAreByteDistinct(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	n, inner, _ := newTestNetwork(&fakeSigner{})
	msg := prefsMsg(t, 200)
	honestRoot := decodePartial(t, msg).Messages[0].SigningRoot

	const repeat = 5
	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 200, Resign: true, Repeat: repeat, Every: time.Millisecond}})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return inner.count() == repeat+1
	}, time.Second, 5*time.Millisecond)

	seen := make(map[string]bool, repeat+1)
	for i := 0; i <= repeat; i++ {
		data := inner.dataAt(i)
		key := string(data)
		require.False(t, seen[key], "send #%d duplicated an earlier send's bytes — gossipsub would have dropped it", i)
		seen[key] = true

		body := &spectypes.PartialSignatureMessages{}
		require.NoError(t, body.Decode(data))
		require.Equal(t, honestRoot, body.Messages[0].SigningRoot,
			"the signing root — the rule under test — must never move")
	}
}

// TestDispatchPropagatesIdentitySendError pins FIX 5's other half: the identity (unmodified,
// Resign == false) send carries the real duty's honest message, so its failure is a genuine
// broadcast failure and must reach the caller exactly as the stock, undecorated network would
// report it.
func TestDispatchPropagatesIdentitySendError(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	core, logs := observer.New(zapcore.WarnLevel)
	inner := &fakeP2P{failAt: 1}
	n := &Network{P2PNetwork: inner, signer: &fakeSigner{}, logger: zap.New(core)}
	msg := partialMsg(t, spectypes.RoleProposerPreferences, 100)

	err := n.dispatch([]Outgoing{{Msg: msg, Slot: 100, Resign: false}})

	require.Error(t, err, "the identity send's error must propagate to the caller")
	require.Equal(t, 0, logs.FilterMessageSnippet(fireSnippet).Len(), "an unsigned identity send never announces, failed or not")
}

// TestDispatchLogsAndContinuesPastForgedSendError pins FIX 5's main fix: a forged (Resign == true)
// send's broadcast error must be logged, not returned — this is QA instrumentation riding alongside
// the honest duty, and it must never be able to fail that duty by making its own broadcast error
// look like the caller's (BroadcastAtSlot's) error. dispatch must also keep going past the failure,
// the way a stock loop over independent sends would, rather than aborting the rest of the list.
func TestDispatchLogsAndContinuesPastForgedSendError(t *testing.T) {
	faults.SetForTest(t, faults.PrefsReplay)
	core, logs := observer.New(zapcore.WarnLevel)
	// Call 1 is the identity send (succeeds); call 2 is the forged send (fails); call 3 is a second
	// forged send (succeeds) — proving dispatch does not abort the remaining list on the failure.
	inner := &fakeP2P{failAt: 2}
	n := &Network{P2PNetwork: inner, signer: &fakeSigner{}, logger: zap.New(core)}

	honest := partialMsg(t, spectypes.RoleProposerPreferences, 100)
	forgedFails, err := Clone(honest)
	require.NoError(t, err)
	forgedSucceeds, err := Clone(honest)
	require.NoError(t, err)

	dispatchErr := n.dispatch([]Outgoing{
		{Msg: honest, Slot: 100, Resign: false},
		{Msg: forgedFails, Slot: 100, Resign: true},
		{Msg: forgedSucceeds, Slot: 100, Resign: true},
	})

	require.NoError(t, dispatchErr, "a forged send's error must not fail the honest duty")
	require.Equal(t, 2, inner.count(), "the identity send and the second forged send both reached the wire")
	require.Same(t, honest, inner.at(0), "the honest message is untouched and sent first")
	require.Same(t, forgedSucceeds, inner.at(1), "dispatch must continue past the failed forged send to the next entry")
	require.Equal(t, 1, logs.FilterMessageSnippet("forged send failed").Len())
}
