package validator

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestValidatorEnqueueMessageDropsStaleRoundUnderPressure(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	const (
		queueCapacity = 4
		role          = spectypes.RoleProposer
	)

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(2)

	v := testValidatorForQueueAdmission(logger, role, slot, currentRound, true, queueCapacity)
	q := v.Queues[role]

	for i := 0; i < 3; i++ {
		v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, testValidatorMsgID(role, byte(i+1)), &specqbft.Message{
			Height:  specqbft.Height(slot),
			Round:   currentRound,
			MsgType: specqbft.CommitMsgType,
		}))
	}
	require.Equal(t, 3, q.Len())

	staleMsgID := testValidatorMsgID(role, 0xAA)
	v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, staleMsgID, &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   currentRound - 1,
		MsgType: specqbft.PrepareMsgType,
	}))
	require.Equal(t, 3, q.Len())

	currentMsgID := testValidatorMsgID(role, 0xBB)
	v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, currentMsgID, &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   currentRound,
		MsgType: specqbft.ProposalMsgType,
	}))
	require.Equal(t, queueCapacity, q.Len())

	state := v.messageQueueState(currentMsgID, slot)
	foundStale := false
	foundCurrent := false
	for i := 0; i < queueCapacity; i++ {
		popCtx, popCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		msg := q.Pop(popCtx, queue.NewMessagePrioritizer(state), queue.FilterAny)
		popCancel()
		require.NotNil(t, msg)

		if msg.MsgID == staleMsgID {
			foundStale = true
		}
		if msg.MsgID == currentMsgID {
			foundCurrent = true
		}
	}

	assert.False(t, foundStale)
	assert.True(t, foundCurrent)
}

func TestValidatorEnqueueMessageKeepsPreviousSlotPartialSignaturesUnderPressure(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	const (
		queueCapacity = 4
		role          = spectypes.RoleProposer
	)

	slot := phase0.Slot(123)

	v := testValidatorForQueueAdmission(logger, role, slot, specqbft.Round(1), false, queueCapacity)
	q := v.Queues[role]

	for i := 0; i < 3; i++ {
		v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, testValidatorMsgID(role, byte(i+1)), &spectypes.PartialSignatureMessages{
			Type: spectypes.PostConsensusPartialSig,
			Slot: slot,
		}))
	}
	require.Equal(t, 3, q.Len())

	previousSlotMsgID := testValidatorMsgID(role, 0xCC)
	v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, previousSlotMsgID, &spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: slot - 1,
	}))
	require.Equal(t, queueCapacity, q.Len())

	tooOldMsgID := testValidatorMsgID(role, 0xDD)
	v.EnqueueMessage(ctx, makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, tooOldMsgID, &spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: slot - 2,
	}))
	require.Equal(t, queueCapacity, q.Len())

	state := v.messageQueueState(previousSlotMsgID, slot)
	foundPreviousSlot := false
	foundTooOld := false
	for i := 0; i < queueCapacity; i++ {
		popCtx, popCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		msg := q.Pop(popCtx, queue.NewMessagePrioritizer(state), queue.FilterAny)
		popCancel()
		require.NotNil(t, msg)

		if msg.MsgID == previousSlotMsgID {
			foundPreviousSlot = true
		}
		if msg.MsgID == tooOldMsgID {
			foundTooOld = true
		}
	}

	assert.True(t, foundPreviousSlot)
	assert.False(t, foundTooOld)
}

func testValidatorForQueueAdmission(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	slot phase0.Slot,
	round specqbft.Round,
	hasRunningInstance bool,
	queueCapacity int,
) *Validator {
	state := &runner.State{
		CurrentDuty: &spectypes.ValidatorDuty{
			Type: spectypes.BNRoleProposer,
			Slot: slot,
		},
	}
	if hasRunningInstance {
		state.RunningInstance = &instance.Instance{
			State: &specqbft.State{
				Round: round,
			},
		}
	}

	r := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			RunnerRoleType: role,
			QBFTController: &controller.Controller{
				Height: specqbft.Height(slot),
			},
			State: state,
		},
	}

	return &Validator{
		logger:        logger,
		NetworkConfig: networkconfig.TestNetwork,
		Operator:      &spectypes.CommitteeMember{},
		Share: &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex: 1,
			},
		},
		Queues: map[spectypes.RunnerRole]queue.Queue{
			role: queue.New(logger, queueCapacity),
		},
		DutyRunners: runner.ValidatorDutyRunners{
			role: r,
		},
	}
}

func testValidatorMsgID(role spectypes.RunnerRole, discriminator byte) spectypes.MessageID {
	return spectypes.NewMsgID([4]byte{discriminator}, []byte{discriminator}, role)
}
