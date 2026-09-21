package validator

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// While a runner has no running duty, its consumer takes only duty-start events from the queue: a
// partial for a duty this operator has not started yet waits there and is processed once the duty
// starts. This is what keeps the runners' duty-state sentinels out of reach through the queue (see
// runner.ValidatePreConsensusMsg) and lets a slightly early message survive.
func TestConsumeQueue_HoldsMessagesUntilDutyStarts(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	netCfg := networkconfig.TestNetwork
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: phase0.Slot(10)}
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, duty.PubKey[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: proposer},
	}

	partial := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID, &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{{PartialSignature: make([]byte, 96), Signer: 1, ValidatorIndex: 1}},
	})
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partial))

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			// The duty starts: from here on the runner has a running duty and the filter lifts.
			proposer.State = runner.NewRunnerState(3, duty)
		}
		delivered <- msg
		return nil
	})

	select {
	case msg := <-delivered:
		t.Fatalf("message of type %d delivered before the duty started", msg.MsgType)
	case <-time.After(200 * time.Millisecond):
	}

	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, duty.Slot)))

	require.Equal(t, message.SSVEventMsgType, receiveDelivered(t, delivered).MsgType, "the duty-start event goes first")
	require.Equal(t, spectypes.SSVPartialSignatureMsgType, receiveDelivered(t, delivered).MsgType, "the held partial follows once the duty runs")
}

// A duty start purges what is still queued for earlier slots: the tail of the concluded duty (and any
// message for a duty this operator never ran) is dropped without reaching the runner, while a message
// that arrived early for the starting duty is still delivered once the duty runs (issue #3037).
func TestConsumeQueue_DropsStaleMessagesAtDutyStart(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	netCfg := networkconfig.TestNetwork
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: phase0.Slot(10)}
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, duty.PubKey[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: proposer},
	}

	// The tail of the duty at slot 5 that concluded before these arrived, and an early partial for slot 10.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 5)))
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 10)))

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			proposer.State = runner.NewRunnerState(3, duty)
		}
		delivered <- msg
		return nil
	})

	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, duty.Slot)))

	require.Equal(t, message.SSVEventMsgType, receiveDelivered(t, delivered).MsgType, "the duty-start event goes first")
	early := receiveDelivered(t, delivered)
	earlySlot, err := early.Slot()
	require.NoError(t, err)
	require.Equal(t, duty.Slot, earlySlot, "the early partial for the starting duty follows")

	select {
	case msg := <-delivered:
		slot, _ := msg.Slot()
		t.Fatalf("stale message for slot %d reached the handler", slot)
	case <-time.After(200 * time.Millisecond):
	}
	require.True(t, v.Queues[spectypes.RoleProposer].Empty(), "the stale tail is purged, not retained")
}

// The bulk purge at duty start cannot catch a stale message that is not in the queue at that moment —
// one parked in a retry goroutine, or one that simply arrives afterwards. When such a message reaches
// the consumer below the floor, the consumer drops it rather than handing it to the runner, which would
// reject it with the exact "invalid partial sig slot" error the purge exists to prevent (issue #3037).
func TestConsumeQueue_DropsStaleMessageArrivingAfterDutyStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// An observed logger lets the test see the consumer drop the stale message without racing on the
	// queue's single-consumer internals (Empty/Len are not safe to call while the consumer runs).
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	netCfg := networkconfig.TestNetwork
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: phase0.Slot(10)}
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, duty.PubKey[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: proposer},
	}

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			proposer.State = runner.NewRunnerState(3, duty)
		}
		delivered <- msg
		return nil
	})

	// The duty starts, raising the floor to slot 10.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, duty.Slot)))
	require.Equal(t, message.SSVEventMsgType, receiveDelivered(t, delivered).MsgType, "the duty-start event goes first")

	// A stale message for an earlier slot now arrives, after the bulk purge already ran. The consumer
	// drops it rather than handing it to the runner.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 5)))
	require.Eventually(t, func() bool {
		return logs.FilterMessage("dropped a stale message that reached the consumer below the slot floor").Len() == 1
	}, 2*time.Second, 10*time.Millisecond, "the late stale message is dropped by the consumer")

	select {
	case msg := <-delivered:
		slot, _ := msg.Slot()
		t.Fatalf("stale message for slot %d reached the handler", slot)
	case <-time.After(50 * time.Millisecond):
	}
}

// A later, higher-slot duty can re-seat a runner still busy with an earlier one: its duty-start is popped
// mid-duty (the non-idle filter passes events through) and accepted, so the runner advances and the earlier
// duty's tail turns stale. Raising the floor only on the idle path would leave that tail to reach the runner
// and draw the same rejection the purge prevents (issue #3037); the mid-duty re-seat raises the floor too, so
// the pop-guard drops the superseded tail without a bulk purge.
func TestConsumeQueue_RaisesFloorOnMidDutyReseat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	netCfg := networkconfig.TestNetwork
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, pk[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: proposer},
	}

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		// Each duty-start re-seats the runner to its own slot, so a higher-slot duty-start popped while a
		// duty runs advances the runner without it ever going idle.
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			slot, err := msg.Slot()
			require.NoError(t, err)
			proposer.State = runner.NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: slot})
		}
		delivered <- msg
		return nil
	})

	// The runner starts the duty at slot 10 from idle, raising the floor to 10.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, 10)))
	require.Equal(t, message.SSVEventMsgType, receiveDelivered(t, delivered).MsgType, "the slot-10 duty-start starts the duty")

	// While that duty is still running, the slot-20 duty-start is popped and re-seats the runner. It is a
	// duty-start, so it is delivered (never dropped) and raises the floor to 20.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, 20)))
	reseat := receiveDelivered(t, delivered)
	reseatSlot, err := reseat.Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(20), reseatSlot, "the higher-slot duty-start re-seats the busy runner")

	// The tail of the superseded slot-10 duty now reaches the consumer. With the floor at 20 it is dropped
	// rather than handed to the runner, which would reject it with "invalid partial sig slot".
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 10)))
	require.Eventually(t, func() bool {
		return logs.FilterMessage("dropped a stale message that reached the consumer below the slot floor").Len() == 1
	}, 2*time.Second, 10*time.Millisecond, "the superseded duty's tail is dropped after the mid-duty re-seat")

	select {
	case msg := <-delivered:
		slot, _ := msg.Slot()
		t.Fatalf("stale message for slot %d reached the handler after the re-seat", slot)
	case <-time.After(50 * time.Millisecond):
	}
}

// A duty-start can be accepted for a slot below the floor: a runner whose duties have not started a QBFT
// instance keeps LatestInstanceHeight at 0, so ShouldProcessDuty accepts any slot. The floor must follow the
// runner down to the slot it actually accepted, or that duty's own messages are dropped and it silently
// starves. Here the slot-20 duty runs first (floor rises to 20) and concludes without an instance, then the
// slot-10 duty-start is accepted from idle — its messages must still reach the runner (issue #3037).
func TestConsumeQueue_LowerSlotDutyAcceptedAfterFloorRoseIsNotStarved(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	netCfg := networkconfig.TestNetwork
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, pk[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: proposer},
	}

	delivered := make(chan *queue.SSVMessage, 8)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			slot, err := msg.Slot()
			require.NoError(t, err)
			proposer.State = runner.NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: slot})
			// The slot-20 duty concludes without ever starting an instance, so the runner returns to idle with
			// LatestInstanceHeight still 0 — and the later slot-10 start is then accepted rather than rejected.
			if slot == 20 {
				proposer.State.Succeeded = true
			}
		}
		delivered <- msg
		return nil
	})

	// Slot 20 runs first and concludes, raising the floor to 20.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, 20)))
	first, err := receiveDelivered(t, delivered).Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(20), first, "the slot-20 duty runs first")

	// The slot-10 duty-start is accepted from idle, re-seating the runner below the floor.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, 10)))
	second, err := receiveDelivered(t, delivered).Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(10), second, "the slot-10 duty-start is accepted from idle")

	// Its own message must reach the runner: the floor followed the runner down to 10.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 10)))
	third, err := receiveDelivered(t, delivered).Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(10), third, "the accepted slot-10 duty is not starved by a stale floor")
}

// The stale purge drops whatever slotBelow matches, so it must never match a duty-start: dropping one
// would silently skip a duty (issue #3037). Any other message below the floor is fair game to drop.
func TestSlotBelow_NeverMatchesDutyStart(t *testing.T) {
	domain := networkconfig.TestNetwork.DomainType
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(domain, pk[:], spectypes.RoleProposer)

	belowFloor := slotBelow(phase0.Slot(12))
	require.False(t, belowFloor(executeDutyMsg(t, domain, 10)), "a duty-start below the floor is kept")
	require.False(t, belowFloor(executeDutyMsg(t, domain, 20)), "a duty-start above the floor is kept")
	require.True(t, belowFloor(partialSigMsg(t, msgID, 9)), "a stale partial below the floor is purged")
	require.False(t, belowFloor(partialSigMsg(t, msgID, 12)), "the floor is exclusive")
	require.False(t, belowFloor(partialSigMsg(t, msgID, 15)), "a partial above the floor is kept")
}

// A higher-slot duty-start can be popped before a lower-slot one still queued: duty-starts race in from
// per-duty goroutines and tie in the prioritizer. The purge must keep that lower-slot duty-start and
// drop only the genuinely stale tail, else a duty is silently skipped (issue #3037).
func TestPurgeAtDutyStart_KeepsAConcurrentDutyStart(t *testing.T) {
	domain := networkconfig.TestNetwork.DomainType
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(domain, pk[:], spectypes.RoleProposer)

	q := queue.New(zap.NewNop(), 16)
	require.True(t, q.TryPush(executeDutyMsg(t, domain, 12))) // higher-slot duty-start enqueued first...
	require.True(t, q.TryPush(executeDutyMsg(t, domain, 10))) // ...its goroutine won the race
	require.True(t, q.TryPush(partialSigMsg(t, msgID, 9)))    // a genuinely stale tail message

	// The idle consumer pops a duty-start; the tie-break returns the first-enqueued one (slot 12).
	popped := q.TryPop(queue.NewMessagePrioritizer(&queue.State{}), isExecuteDuty)
	require.NotNil(t, popped)
	floor, err := popped.Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(12), floor, "the higher-slot duty-start is popped first")

	require.Equal(t, 1, q.Purge(slotBelow(floor), queue.PurgeReasonStale, nil), "only the stale partial is dropped")

	remaining := q.TryPop(queue.NewMessagePrioritizer(&queue.State{}), queue.FilterAny)
	require.NotNil(t, remaining, "the lower-slot duty-start survives the purge")
	require.True(t, isExecuteDuty(remaining))
	remainingSlot, err := remaining.Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(10), remainingSlot)
	require.True(t, q.Empty())
}

// executeDutyMsg builds a decoded proposer duty-start event for the given slot.
func executeDutyMsg(t *testing.T, domain spectypes.DomainType, slot phase0.Slot) *queue.SSVMessage {
	t.Helper()
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: slot}
	raw, err := createDutyExecuteMsg(duty, duty.PubKey, domain, spectypes.RoleProposer)
	require.NoError(t, err)
	decoded, err := queue.DecodeSSVMessage(raw)
	require.NoError(t, err)
	return decoded
}

// partialSigMsg builds a post-consensus partial-signature message for the given slot.
func partialSigMsg(t *testing.T, msgID spectypes.MessageID, slot phase0.Slot) *queue.SSVMessage {
	t.Helper()
	return makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID, &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{{PartialSignature: make([]byte, 96), Signer: 1, ValidatorIndex: 1}},
	})
}

func receiveDelivered(t *testing.T, delivered <-chan *queue.SSVMessage) *queue.SSVMessage {
	t.Helper()
	select {
	case msg := <-delivered:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no message delivered")
		return nil
	}
}

// The stale floor is a single-duty notion. A runner that serves several slots at once (runner.MultiSlotRunner)
// never raises it: a message for a lower slot stays live after a higher slot's duty-start, where a single-duty
// runner would have it dropped (compare TestConsumeQueue_DropsStaleMessageArrivingAfterDutyStart).
func TestConsumeQueue_MultiSlotRunnerNeverRaisesTheFloor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	netCfg := networkconfig.TestNetwork
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, pk[:], spectypes.RoleProposer)
	proposer := &runner.ProposerRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposer}}

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposer: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposer: multiSlotRunnerStub{proposer}},
	}

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			slot, err := msg.Slot()
			require.NoError(t, err)
			proposer.State = runner.NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: slot})
		}
		delivered <- msg
		return nil
	})

	// The slot-20 duty starts; a single-duty runner would now have its floor at 20.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(executeDutyMsg(t, netCfg.DomainType, 20)))
	require.Equal(t, message.SSVEventMsgType, receiveDelivered(t, delivered).MsgType, "the duty-start event goes first")

	// A slot-10 message still reaches the runner: for it that slot is as live as slot 20.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialSigMsg(t, msgID, 10)))
	got, err := receiveDelivered(t, delivered).Slot()
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(10), got, "the lower slot's message is delivered, not dropped as stale")
	require.Zero(t, logs.FilterMessage("dropped a stale message that reached the consumer below the slot floor").Len())
}

// popFilter is the consumer's whole pop policy: idle, only duty-starts; an instance running without an accepted
// proposal for its round, that round's prepares and commits stay queued; otherwise anything goes.
func TestPopFilter(t *testing.T) {
	executeDuty := &queue.SSVMessage{Body: &ssvtypes.EventMsg{Type: ssvtypes.ExecuteDuty}}
	proposal := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.ProposalMsgType, Height: 8, Round: 2}}
	prepare := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.PrepareMsgType, Height: 8, Round: 2}}
	commit := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.CommitMsgType, Height: 8, Round: 2}}
	roundChange := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.RoundChangeMsgType, Height: 8, Round: 2}}
	nextRoundPrepare := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.PrepareMsgType, Height: 8, Round: 3}}
	nextHeightCommit := &queue.SSVMessage{Body: &specqbft.Message{MsgType: specqbft.CommitMsgType, Height: 9, Round: 2}}
	postConsensus := &queue.SSVMessage{Body: &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: 8}}
	all := []*queue.SSVMessage{executeDuty, proposal, prepare, commit, roundChange, nextRoundPrepare, nextHeightCommit, postConsensus}

	instanceAtRound2 := &queue.State{HasRunningInstance: true, Slot: 8, Round: 2}
	noInstance := &queue.State{HasRunningInstance: false, Slot: 8, Round: 2}

	check := func(t *testing.T, filter queue.Filter, pass func(m *queue.SSVMessage) bool) {
		for i, m := range all {
			require.Equalf(t, pass(m), filter(m), "message %d (%T)", i, m.Body)
		}
	}

	t.Run("idle: duty-starts only", func(t *testing.T) {
		check(t, popFilter(plainRunnerStub{}, true, false, instanceAtRound2), func(m *queue.SSVMessage) bool { return m == executeDuty })
	})
	t.Run("idle multi-slot runner: anything goes", func(t *testing.T) {
		check(t, popFilter(multiSlotRunnerStub{}, true, true, noInstance), func(*queue.SSVMessage) bool { return true })
	})
	t.Run("instance running, no proposal accepted: its round's prepares and commits wait", func(t *testing.T) {
		check(t, popFilter(proposalRunnerStub{accepted: false}, false, false, instanceAtRound2), func(m *queue.SSVMessage) bool { return m != prepare && m != commit })
	})
	t.Run("proposal accepted: anything goes", func(t *testing.T) {
		check(t, popFilter(proposalRunnerStub{accepted: true}, false, false, instanceAtRound2), func(*queue.SSVMessage) bool { return true })
	})
	t.Run("duty running before its instance: anything goes", func(t *testing.T) {
		check(t, popFilter(proposalRunnerStub{accepted: false}, false, false, noInstance), func(*queue.SSVMessage) bool { return true })
	})
}

// A runner that serves several slots at once (runner.MultiSlotRunner) is never held on the idle filter: its
// partials go through while it reports no running duty, where a single-duty runner's wait for the next
// duty-start (TestConsumeQueue_HoldsMessagesUntilDutyStarts). The hold starved the preferences dispatcher after
// its first quorum: it reports no running duty as soon as its only started slot's preference concludes, yet
// that slot's sub-runner still collects request-auth partials, so a peer's share arriving after our quorum sat
// in the queue until the next preference duty, epochs later, and its auth root never reconstructed.
func TestConsumeQueue_MultiSlotRunnerIsNotHeldWhileIdle(t *testing.T) {
	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	netCfg := networkconfig.TestNetwork
	const slot = phase0.Slot(100)
	var pk phase0.BLSPubKey
	msgID := ssvtestingutils.NewMsgID(netCfg.DomainType, pk[:], spectypes.RoleProposerPreferences)
	// The real dispatcher with no running sub-runner, the state a concluded preference leaves it in.
	dispatcher := &runner.ProposerPreferencesRunner{BaseRunner: &runner.BaseRunner{RunnerRoleType: spectypes.RoleProposerPreferences}}
	require.False(t, dispatcher.HasRunningDuty())

	v := &Validator{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		NetworkConfig: netCfg,
		Operator:      &spectypes.CommitteeMember{},
		Share:         &ssvtypes.SSVShare{},
		Queues:        map[spectypes.RunnerRole]queue.Queue{spectypes.RoleProposerPreferences: queue.New(logger, 16)},
		DutyRunners:   runner.ValidatorDutyRunners{spectypes.RoleProposerPreferences: dispatcher},
	}

	// A peer's request-auth share and another peer's late preference share for the slot whose preference we
	// already concluded.
	for _, typ := range []spectypes.PartialSigMsgType{spectypes.RequestAuthPartialSig, spectypes.ProposerPreferencesPartialSig} {
		partial := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID, &spectypes.PartialSignatureMessages{
			Type:     typ,
			Slot:     slot,
			Messages: []*spectypes.PartialSignatureMessage{{PartialSignature: make([]byte, 96), Signer: 3, ValidatorIndex: 1}},
		})
		require.True(t, v.Queues[spectypes.RoleProposerPreferences].TryPush(partial))
	}

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		delivered <- msg
		return nil
	})

	got := map[spectypes.PartialSigMsgType]bool{}
	for range 2 {
		partial, ok := receiveDelivered(t, delivered).Body.(*spectypes.PartialSignatureMessages)
		require.True(t, ok, "a partial is delivered while the dispatcher reports no running duty")
		got[partial.Type] = true
	}
	require.True(t, got[spectypes.RequestAuthPartialSig], "the request-auth share reaches the dispatcher")
	require.True(t, got[spectypes.ProposerPreferencesPartialSig], "so does the preference share")
}

// plainRunnerStub is a runner that never awaits post-consensus packets once its duty is done.
type plainRunnerStub struct{ runner.Runner }

// proposalRunnerStub is a runner whose current round may or may not have an accepted proposal.
type proposalRunnerStub struct {
	runner.Runner
	accepted bool
}

func (s proposalRunnerStub) HasAcceptedProposalForCurrentRound() bool { return s.accepted }

// multiSlotRunnerStub serves several slots at once, like the proposer-preferences dispatcher.
type multiSlotRunnerStub struct{ runner.Runner }

func (multiSlotRunnerStub) ServesMultipleSlots() {}

// awaitingRunnerStub is a runner whose finished duty may still expect post-consensus packets.
type awaitingRunnerStub struct {
	runner.Runner
	slot     phase0.Slot
	awaiting bool
}

func (s awaitingRunnerStub) AwaitingPostConsensus() (phase0.Slot, bool) { return s.slot, s.awaiting }

// The hold above has one exception: a finished duty still expecting post-consensus packets (the Gloas
// proposer's §6 envelope root) gets that slot's post-consensus packets, and nothing else.
func TestNoRunningDutyFilter(t *testing.T) {
	executeDuty := &queue.SSVMessage{Body: &ssvtypes.EventMsg{Type: ssvtypes.ExecuteDuty}}
	timeout := &queue.SSVMessage{Body: &ssvtypes.EventMsg{Type: ssvtypes.Timeout}}
	consensus := &queue.SSVMessage{Body: &specqbft.Message{Height: 8}}
	preConsensus := &queue.SSVMessage{Body: &spectypes.PartialSignatureMessages{Type: spectypes.RandaoPartialSig, Slot: 8}}
	postConsensus := &queue.SSVMessage{Body: &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: 8}}
	nextSlotPostConsensus := &queue.SSVMessage{Body: &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: 9}}

	for name, r := range map[string]runner.Runner{
		"plain runner":         plainRunnerStub{},
		"awaiter not awaiting": awaitingRunnerStub{slot: 8, awaiting: false},
	} {
		t.Run(name, func(t *testing.T) {
			filter := noRunningDutyFilter(r)
			require.True(t, filter(executeDuty))
			for _, held := range []*queue.SSVMessage{timeout, consensus, preConsensus, postConsensus, nextSlotPostConsensus} {
				require.False(t, filter(held))
			}
		})
	}

	t.Run("awaiting post-consensus", func(t *testing.T) {
		filter := noRunningDutyFilter(awaitingRunnerStub{slot: 8, awaiting: true})
		require.True(t, filter(executeDuty))
		require.True(t, filter(postConsensus))
		for _, held := range []*queue.SSVMessage{timeout, consensus, preConsensus, nextSlotPostConsensus} {
			require.False(t, filter(held))
		}
	})
}
