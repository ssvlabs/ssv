package validator

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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
	msgID := spectypes.NewMsgID(netCfg.DomainType, duty.PubKey[:], spectypes.RoleProposer)
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

	executeDuty, err := createDutyExecuteMsg(duty, duty.PubKey, netCfg.DomainType, spectypes.RoleProposer)
	require.NoError(t, err)
	decoded, err := queue.DecodeSSVMessage(executeDuty)
	require.NoError(t, err)
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(decoded))

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
	msgID := spectypes.NewMsgID(netCfg.DomainType, duty.PubKey[:], spectypes.RoleProposer)
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

	partialAt := func(slot phase0.Slot) *queue.SSVMessage {
		return makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID, &spectypes.PartialSignatureMessages{
			Type:     spectypes.PostConsensusPartialSig,
			Slot:     slot,
			Messages: []*spectypes.PartialSignatureMessage{{PartialSignature: make([]byte, 96), Signer: 1, ValidatorIndex: 1}},
		})
	}
	// The tail of the duty at slot 5 that concluded before these arrived, and an early partial for slot 10.
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialAt(5)))
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(partialAt(10)))

	delivered := make(chan *queue.SSVMessage, 4)
	v.StartQueueConsumer(msgID, func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		if event, ok := msg.Body.(*ssvtypes.EventMsg); ok && event.Type == ssvtypes.ExecuteDuty {
			proposer.State = runner.NewRunnerState(3, duty)
		}
		delivered <- msg
		return nil
	})

	executeDuty, err := createDutyExecuteMsg(duty, duty.PubKey, netCfg.DomainType, spectypes.RoleProposer)
	require.NoError(t, err)
	decoded, err := queue.DecodeSSVMessage(executeDuty)
	require.NoError(t, err)
	require.True(t, v.Queues[spectypes.RoleProposer].TryPush(decoded))

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
