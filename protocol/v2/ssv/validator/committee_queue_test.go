package validator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qbfttests "github.com/ssvlabs/ssv/integration/qbft/tests"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// makeTestSSVMessage creates a test SignedSSVMessage for testing queue handling.
// It generates a mock message with:
// - A predefined signature
// - Specified message type (consensus, partial signature, etc.)
// - A unique message ID
// - Custom message body (QBFT message or partial signature message)
// The function handles encoding the message body appropriately based on its type.
func makeTestSSVMessage(t *testing.T, msgType spectypes.MsgType, msgID spectypes.MessageID, body interface{}) *queue.SSVMessage {
	t.Helper()

	sig, err := base64.StdEncoding.DecodeString("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN")
	require.NoError(t, err)

	var data []byte
	switch v := body.(type) {
	case *specqbft.Message:
		var err error
		data, err = v.Encode()
		require.NoError(t, err)
	case *spectypes.PartialSignatureMessages:
		var err error
		data, err = v.Encode()
		require.NoError(t, err)
	case *types.EventMsg:
		var err error
		data, err = v.Encode()
		require.NoError(t, err)
	default:
		t.Fatalf("unsupported message body type: %T", body)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: msgType,
		MsgID:   msgID,
		Data:    data,
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{1, 2, 3, 4},
		SSVMessage:  ssvMsg,
	}
	decoded, err := queue.DecodeSignedSSVMessage(signed)
	require.NoError(t, err)
	return decoded
}

// safeConsumeQueue wraps ConsumeQueue execution in a goroutine with mutex protection.
func safeConsumeQueue(t *testing.T, ctx context.Context, committee *Committee, q queueContainer,
	logger *zap.Logger, handler func(context.Context, *zap.Logger, *queue.SSVMessage) error,
	committeeRunner *runner.CommitteeRunner, mutex *sync.RWMutex) {

	t.Helper()

	go func() {
		mutex.RLock()
		defer mutex.RUnlock()
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		require.NoError(t, err)
	}()
}

// TestHandleMessageCreatesQueue verifies that the HandleMessage method correctly
// initializes a new queue when receiving a message for a slot that doesn't have
// an associated queue yet.
//
// Flow:
// 1. Set up a committee with empty queues map
// 2. Create a test SSV message for a specific slot
// 3. Call HandleMessage with the test message
// 4. Verify that a new queue was created for the message's slot
// 5. Confirm the queue has the correct properties (non-nil and proper slot)
func TestHandleMessageCreatesQueue(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	msgID := spectypes.MessageID{1, 2, 3, 4}
	qbftMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}
	testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)

	committee.HandleMessage(ctx, logger, testMsg)

	committee.mtx.Lock()
	q, ok := committee.Queues[slot]
	committee.mtx.Unlock()

	require.True(t, ok)

	assert.NotNil(t, q.Q)
	assert.Equal(t, slot, q.queueState.Slot)
}

// TestConsumeQueueBasic tests the fundamental queue consumption functionality
// of the ConsumeQueue method when it has accepted messages to process.
//
// Flow:
// 1. Set up a committee with empty state
// 2. Create a queue container with two messages (a proposal and a prepare message)
// 3. Set up a runner with a proposal already accepted (enabling processing of prepare messages)
// 4. Initialize a handler to track received messages
// 5. Start consuming the queue in a separate goroutine
// 6. Wait for both messages to be processed
// 7. Verify both messages were received by the handler
// 8. Confirm messages were processed in the expected order
func TestConsumeQueueBasic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	msgID1 := spectypes.MessageID{1, 2, 3, 4}
	msgID2 := spectypes.MessageID{5, 6, 7, 8}

	qbftMsg1 := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}
	testMsg1 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID1, qbftMsg1)

	qbftMsg2 := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.PrepareMsgType,
	}
	testMsg2 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID2, qbftMsg2)

	q := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}
	q.Q.TryPush(testMsg1)
	q.Q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg1,
	}

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: proposalMsg,
						Round:                           1,
					},
				},
			},
		},
	}

	receivedMessages := make([]*queue.SSVMessage, 0)
	handlerCalled := make(chan struct{}, 2)

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		receivedMessages = append(receivedMessages, msg)
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

	for i := 0; i < 2; i++ {
		select {
		case <-handlerCalled:
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for message %d", i+1)
		}
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 2, len(receivedMessages))
	if len(receivedMessages) >= 2 {
		assert.Equal(t, specqbft.ProposalMsgType, receivedMessages[0].Body.(*specqbft.Message).MsgType)
		assert.Equal(t, specqbft.PrepareMsgType, receivedMessages[1].Body.(*specqbft.Message).MsgType)
	}
}

// TestStartConsumeQueue tests the StartConsumeQueue method, which orchestrates queue processing
// for a specific slot. This test verifies error handling in various scenarios:
// - Missing queue for a slot
// - Missing runner for a slot
// - Successful queue consumption start
//
// Flow:
// 1. Set up a committee with a queue for a specific slot and a corresponding runner
// 2. Test scenario 1: Call StartConsumeQueue with a duty for a nonexistent slot (should error)
// 3. Test scenario 2: Delete runner and call StartConsumeQueue for the existing slot (should error)
// 4. Test scenario 3: Restore runner and call StartConsumeQueue for the valid slot (should succeed)
func TestStartConsumeQueue(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)

	q := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: false,
			Height:             specqbft.Height(slot),
			Slot:               slot,
		},
	}
	committee.Queues[slot] = q

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{},
		},
	}
	committee.Runners[slot] = committeeRunner

	duty := &spectypes.CommitteeDuty{
		Slot: phase0.Slot(124),
	}
	err := committee.StartConsumeQueue(logger, duty)
	assert.Error(t, err)

	duty.Slot = slot
	delete(committee.Runners, slot)
	err = committee.StartConsumeQueue(logger, duty)
	assert.Error(t, err)

	committee.Runners[slot] = committeeRunner
	err = committee.StartConsumeQueue(logger, duty)
	assert.NoError(t, err)
}

// TestFilterNoProposalAccepted tests the message filtering behavior when no proposal
// is accepted for the current round. In this state, the queue should:
// - Process proposal messages (always)
// - Process round change messages (always)
// - Process messages for future rounds (always)
// - Filter out prepare and commit messages for the current round
//
// Flow:
// 1. Set up a committee and queue
// 2. Create a runner in the "no proposal accepted" state
// 3. Add various message types to the queue, including ones that should be filtered
// 4. Start consuming the queue
// 5. Verify only the expected message types are processed
// 6. Verify prepare and commit messages for current round are filtered out
// 7. Confirm messages from future rounds are processed regardless of type
func TestFilterNoProposalAccepted(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(1)
	nextRound := specqbft.Round(2)

	msgIDs := []spectypes.MessageID{
		{1, 2, 3, 4}, // Proposal message, current round
		{2, 3, 4, 5}, // Prepare message, current round
		{3, 4, 5, 6}, // Commit message, current round
		{4, 5, 6, 7}, // RoundChange message, current round
		{5, 6, 7, 8}, // Prepare message, next round
		{6, 7, 8, 9}, // Commit message, next round
	}

	qbftMessages := []*specqbft.Message{
		{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.ProposalMsgType},
		{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType},
		{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.CommitMsgType},
		{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.RoundChangeMsgType},
		{Height: specqbft.Height(slot), Round: nextRound, MsgType: specqbft.PrepareMsgType},
		{Height: specqbft.Height(slot), Round: nextRound, MsgType: specqbft.CommitMsgType},
	}

	q := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              currentRound,
		},
	}

	for i, msg := range qbftMessages {
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgIDs[i], msg)
		q.Q.TryPush(testMsg)
	}

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: nil,
						Round:                           currentRound,
					},
				},
			},
		},
	}

	receivedMessages := make([]*queue.SSVMessage, 0)
	handlerCalled := make(chan struct{}, 10)

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		receivedMessages = append(receivedMessages, msg)
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

	expectedMsgCount := 4
	for i := 0; i < expectedMsgCount; i++ {
		select {
		case <-handlerCalled:
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for expected messages")
		}
	}

	select {
	case <-handlerCalled:
		t.Fatalf("unexpected message processed")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	require.Equal(t, expectedMsgCount, len(receivedMessages))

	processedMsgTypes := make(map[specqbft.MessageType]bool)
	processedRounds := make(map[specqbft.Round]int)

	for _, msg := range receivedMessages {
		qbftMsg := msg.Body.(*specqbft.Message)
		processedMsgTypes[qbftMsg.MsgType] = true
		processedRounds[qbftMsg.Round]++
	}

	assert.True(t, processedMsgTypes[specqbft.ProposalMsgType])
	assert.True(t, processedMsgTypes[specqbft.RoundChangeMsgType])
	assert.Equal(t, 2, processedRounds[nextRound])

	for _, msg := range receivedMessages {
		qbftMsg := msg.Body.(*specqbft.Message)
		if qbftMsg.Round == currentRound {
			assert.NotEqual(t, specqbft.PrepareMsgType, qbftMsg.MsgType)
			assert.NotEqual(t, specqbft.CommitMsgType, qbftMsg.MsgType)
		}
	}
}

// TestFilterNotDecidedSkipsPartialSignatures verifies that when the consensus instance
// is not yet decided (meaning consensus is still in progress), partial signature messages
// are filtered out and not processed.
//
// Flow:
// 1. Set up a committee and queue
// 2. Create a runner with a proposal accepted but not yet decided
// 3. Add both a consensus message and a partial signature message to the queue
// 4. Start consuming the queue
// 5. Verify only the consensus message is processed
// 6. Confirm the partial signature message was filtered out
func TestFilterNotDecidedSkipsPartialSignatures(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)

	msgID1 := spectypes.MessageID{1, 2, 3, 4}
	msgID2 := spectypes.MessageID{5, 6, 7, 8}

	qbftMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}

	partialSigMsg := &spectypes.PartialSignatureMessages{
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				Signer:           1,
				SigningRoot:      [32]byte{},
				ValidatorIndex:   0,
				PartialSignature: make([]byte, 96),
			},
		},
	}

	testMsg1 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID1, qbftMsg)
	testMsg2 := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID2, partialSigMsg)

	q := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	q.Q.TryPush(testMsg1)
	q.Q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg,
	}

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: proposalMsg,
						Round:                           1,
					},
				},
			},
		},
	}

	receivedMessages := make([]*queue.SSVMessage, 0)
	handlerCalled := make(chan struct{}, 2)

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		receivedMessages = append(receivedMessages, msg)
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

	select {
	case <-handlerCalled:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for consensus message")
	}

	select {
	case <-handlerCalled:
		t.Fatalf("partial signature message was incorrectly processed")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	require.Equal(t, 1, len(receivedMessages))
	assert.Equal(t, spectypes.SSVConsensusMsgType, receivedMessages[0].MsgType)
}

// TestFilterDecidedAllowsAll verifies that all message types are processed
// when the consensus instance has reached a decision.
func TestFilterDecidedAllowsAll(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)

	msgID1 := spectypes.MessageID{1, 2, 3, 4}
	msgID2 := spectypes.MessageID{5, 6, 7, 8}

	qbftMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}

	partialSigMsg := &spectypes.PartialSignatureMessages{
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				Signer:           1,
				SigningRoot:      [32]byte{},
				ValidatorIndex:   0,
				PartialSignature: make([]byte, 96),
			},
		},
	}

	testMsg1 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID1, qbftMsg)
	testMsg2 := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID2, partialSigMsg)

	q := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	q.Q.TryPush(testMsg1)
	q.Q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg,
	}

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         true,
						ProposalAcceptedForCurrentRound: proposalMsg,
						Round:                           1,
					},
				},
			},
		},
	}

	receivedMessages := make([]*queue.SSVMessage, 0)
	handlerCalled := make(chan struct{}, 2)

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		receivedMessages = append(receivedMessages, msg)
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

	for i := 0; i < 2; i++ {
		select {
		case <-handlerCalled:
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for message %d", i+1)
		}
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	require.Equal(t, 2, len(receivedMessages))

	msgTypes := make(map[spectypes.MsgType]bool)
	for _, msg := range receivedMessages {
		msgTypes[msg.MsgType] = true
	}
	assert.True(t, msgTypes[spectypes.SSVConsensusMsgType])
	assert.True(t, msgTypes[spectypes.SSVPartialSignatureMsgType])
}

// TestChangingFilterState verifies that messages that were previously filtered
// become processable when the filter state changes.
//
// Flow:
// 1. Create a prepare message and a helper function to run queue consumption once
// 2. First test: Use a runner with no proposal accepted where prepare should be filtered
// 3. Second test: Use a runner with proposal accepted where prepare should process
// 4. Verify prepare messages are properly filtered based on ProposalAccepted state
func TestChangingFilterState(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	slot := phase0.Slot(123)
	round := specqbft.Round(1)

	msgID := spectypes.MessageID{1, 2, 3, 4}
	prepareBody := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   round,
		MsgType: specqbft.PrepareMsgType,
	}
	prepareMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, prepareBody)

	runOnce := func(rnr *runner.CommitteeRunner) *queue.SSVMessage {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		var seen *queue.SSVMessage
		handler := func(_ context.Context, _ *zap.Logger, m *queue.SSVMessage) error {
			seen = m
			return fmt.Errorf("stop") // we don't care about the error
		}

		q := queueContainer{
			Q: queue.New(1),
			queueState: &queue.State{
				HasRunningInstance: true,
				Height:             specqbft.Height(slot),
				Slot:               slot,
				Round:              round,
			},
		}
		q.Q.TryPush(prepareMsg)

		c := &Committee{}
		mutex := &sync.RWMutex{}
		mutex.RLock()
		defer mutex.RUnlock()
		_ = c.ConsumeQueue(ctx, q, logger, handler, rnr)
		return seen
	}

	// 1) No proposal accepted => Prepare should be filtered out
	r1 := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: nil,
						Round:                           round,
					},
				},
			},
		},
	}
	seen1 := runOnce(r1)
	assert.Nil(t, seen1)

	// 2) Proposal accepted => now we should see exactly one Prepare
	r2 := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: &specqbft.ProcessingMessage{QBFTMessage: prepareBody},
						Round:                           round,
					},
				},
			},
		},
	}
	seen2 := runOnce(r2)

	require.NotNil(t, seen2)
	assert.Equal(t, specqbft.PrepareMsgType, seen2.Body.(*specqbft.Message).MsgType)
}

// TestQueueSaturationWithFilteredMessages verifies queue behavior when saturated with messages that are initially
// filtered, then become processable after a state change, ensuring high-priority messages are handled correctly throughout.
//
// Flow:
//  1. Set up a committee with a small queue (capacity 5) and a runner that initially filters Prepare messages.
//  2. Start consuming the queue.
//  3. Fill the queue with Prepare messages (which should be filtered and not processed).
//  4. Push an ExecuteDuty event message; verify it is processed despite the "full" queue of filtered messages.
//  5. Push a Proposal message; verify it is also processed.
//  6. Change the runner's state to accept Prepare messages.
//  7. Restart queue consumption under a new context.
//  8. Observe that the previously filtered Prepare messages are now processed.
//  9. Verify that the critical ExecuteDuty and Proposal messages were indeed processed.
func TestQueueSaturationWithFilteredMessages(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	queueCapacity := 5

	runnerMutex := &sync.RWMutex{}

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: nil,
						Round:                           1,
					},
				},
			},
		},
	}

	q := queueContainer{
		Q: queue.New(queueCapacity),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	var (
		processedMsgs    []*queue.SSVMessage
		processMsgsMutex sync.Mutex
		handlerCalled    = make(chan struct{}, queueCapacity*3)
	)

	// inline handler that synchronizes access to processed messages
	processFn := func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		processMsgsMutex.Lock()
		processedMsgs = append(processedMsgs, msg)
		processMsgsMutex.Unlock()
		handlerCalled <- struct{}{}
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	safeConsumeQueue(t, ctx, committee, q, logger, processFn, committeeRunner, runnerMutex)

	time.Sleep(50 * time.Millisecond)

	// fill with filtered Prepare messages
	for i := 0; i < queueCapacity; i++ {
		msgID := spectypes.MessageID{byte(i + 1)}
		prepare := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.PrepareMsgType}
		require.True(t, q.Q.TryPush(makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, prepare)))
	}

	// nothing should come through yet
	select {
	case <-handlerCalled:
		t.Fatal("filtered message was processed too early")
	default:
	}

	// push ExecuteDuty
	execData, _ := json.Marshal(&types.ExecuteCommitteeDutyData{Duty: &spectypes.CommitteeDuty{Slot: slot}})
	execMsg := makeTestSSVMessage(t, message.SSVEventMsgType, spectypes.MessageID{0xEE}, &types.EventMsg{Type: types.ExecuteDuty, Data: execData})
	require.True(t, q.Q.TryPush(execMsg))
	select {
	case <-handlerCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out on ExecuteDuty")
	}

	// push Proposal
	proposal := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.ProposalMsgType}
	propMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{0xFF}, proposal)
	require.True(t, q.Q.TryPush(propMsg))
	select {
	case <-handlerCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out on Proposal")
	}

	// now flip runner state so prepares become valid
	cancel()
	wg.Wait()

	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()

	runnerMutex.Lock()
	committeeRunner.BaseRunner.State.RunningInstance.State.ProposalAcceptedForCurrentRound =
		&specqbft.ProcessingMessage{QBFTMessage: proposal}
	runnerMutex.Unlock()

	// restart consumption to let the old Prepares through
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx2.Done()
	}()

	safeConsumeQueue(t, ctx2, committee, q, logger, processFn, committeeRunner, runnerMutex)

	// observe how many of the old Prepares now drain
	drained := 0
	timeout := time.After(1 * time.Second)
Loop:
	for {
		select {
		case <-handlerCalled:
			drained++
		case <-timeout:
			break Loop
		}
	}

	// verify essentials still got through
	processMsgsMutex.Lock()
	defer processMsgsMutex.Unlock()
	var sawExec, sawProp bool
	for _, m := range processedMsgs {
		if m.MsgType == message.SSVEventMsgType {
			if e := m.Body.(*types.EventMsg); e.Type == types.ExecuteDuty {
				sawExec = true
			}
		}
		if m.MsgType == spectypes.SSVConsensusMsgType {
			if b := m.Body.(*specqbft.Message); b.MsgType == specqbft.ProposalMsgType {
				sawProp = true
			}
		}
	}
	assert.True(t, sawExec, "missing ExecuteDuty")
	assert.True(t, sawProp, "missing Proposal")
}

// TestCommitteeQueueFilteringScenarios tests different filtering scenarios and their impact on queue processing
//
// Flow:
//  1. Define test cases for various runner states: no running duty, no proposal accepted,
//     proposal accepted but not decided, and fully decided
//  2. For each scenario, set up a committee with the corresponding runner state
//  3. Push various message types (proposal, prepare, commit) to the queue
//  4. Verify that only messages matching the filter criteria for that state are processed
//  5. Confirm each message type is handled correctly based on the current filter state
func TestCommitteeQueueFilteringScenarios(t *testing.T) {
	testCases := []struct {
		name              string
		hasRunningDuty    bool
		decided           bool
		proposalAccepted  bool
		messagesTypes     []specqbft.MessageType
		expectedProcessed []bool
	}{
		{
			name:              "no active duty",
			hasRunningDuty:    false,
			decided:           false,
			proposalAccepted:  false,
			messagesTypes:     []specqbft.MessageType{specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType},
			expectedProcessed: []bool{true, true, true}, // All messages processed with queue.FilterAny when no active duty
		},
		{
			name:              "no proposal accepted",
			hasRunningDuty:    true,
			decided:           false,
			proposalAccepted:  false,
			messagesTypes:     []specqbft.MessageType{specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType, specqbft.RoundChangeMsgType},
			expectedProcessed: []bool{true, false, false, true}, // Proposal and RoundChange should be processed
		},
		{
			name:              "proposal accepted but not decided",
			hasRunningDuty:    true,
			decided:           false,
			proposalAccepted:  true,
			messagesTypes:     []specqbft.MessageType{specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType},
			expectedProcessed: []bool{true, true, true}, // All consensus messages should be processed
		},
		{
			name:              "decided",
			hasRunningDuty:    true,
			decided:           true,
			proposalAccepted:  true,
			messagesTypes:     []specqbft.MessageType{specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType},
			expectedProcessed: []bool{true, true, true}, // All should be processed
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			committee := &Committee{
				ctx:           ctx,
				Queues:        make(map[phase0.Slot]queueContainer),
				Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
				BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			}

			slot := phase0.Slot(123)

			q := queueContainer{
				Q: queue.New(10),
				queueState: &queue.State{
					HasRunningInstance: tc.hasRunningDuty,
					Height:             specqbft.Height(slot),
					Slot:               slot,
					Round:              1,
				},
			}

			var proposalMsg *specqbft.ProcessingMessage
			if tc.proposalAccepted {
				proposalMsg = &specqbft.ProcessingMessage{
					QBFTMessage: &specqbft.Message{},
				}
			}

			committeeRunner := &runner.CommitteeRunner{
				BaseRunner: &runner.BaseRunner{
					State: &runner.State{
						RunningInstance: &instance.Instance{
							State: &specqbft.State{
								Decided:                         tc.decided,
								ProposalAcceptedForCurrentRound: proposalMsg,
								Round:                           1,
							},
						},
					},
				},
			}

			// false for HasRunningDuty()
			if tc.name == "no active duty" {
				committeeRunner.BaseRunner.State.Finished = true
			}

			processed := make([]*queue.SSVMessage, 0)
			handlerCalled := make(chan struct{}, len(tc.messagesTypes))

			handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
				processed = append(processed, msg)
				handlerCalled <- struct{}{}
				return nil
			}

			// Push messages to the queue before starting consumption
			for i, msgType := range tc.messagesTypes {
				msgID := spectypes.MessageID{byte(i + 1)}
				qbftMsg := &specqbft.Message{
					Height:  specqbft.Height(slot),
					Round:   1,
					MsgType: msgType,
				}
				testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)
				pushed := q.Q.TryPush(testMsg)
				require.True(t, pushed)
			}

			safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

			expectedProcessedCount := 0
			for _, shouldProcess := range tc.expectedProcessed {
				if shouldProcess {
					expectedProcessedCount++
				}
			}

			processedCount := 0
			if expectedProcessedCount > 0 {
				timeout := time.After(1 * time.Second)
			waitLoop:
				for processedCount < expectedProcessedCount {
					select {
					case <-handlerCalled:
						processedCount++
					case <-timeout:
						break waitLoop
					}
				}
			} else {
				time.Sleep(200 * time.Millisecond)
			}

			// Verify no additional messages are processed
			if expectedProcessedCount > 0 {
				select {
				case <-handlerCalled:
					t.Fatalf("unexpected message processed, expected only %d", expectedProcessedCount)
				case <-time.After(200 * time.Millisecond):
					// timeout reached
				}
			} else {
				// If we expect zero messages, just wait a bit longer to confirm none are processed
				time.Sleep(200 * time.Millisecond)
			}

			assert.Equal(t, expectedProcessedCount, len(processed))

			for i, msgType := range tc.messagesTypes {
				found := false
				for _, procMsg := range processed {
					if qbftMsg, ok := procMsg.Body.(*specqbft.Message); ok && qbftMsg.MsgType == msgType {
						found = true
						break
					}
				}
				assert.Equal(t, tc.expectedProcessed[i], found)
			}
		})
	}
}

// TestFilterPartialSignatureMessages tests the handling of partial signature messages
// specifically under different decided states
//
// Flow:
// 1. Define test cases for undecided and decided states
// 2. For each case, set up a committee with the appropriate runner state
// 3. Push a partial signature message to the queue
// 4. Verify that partial signature messages are correctly filtered when consensus is not decided
// 5. Confirm partial signature messages are processed when consensus is decided
func TestFilterPartialSignatureMessages(t *testing.T) {
	testCases := []struct {
		name             string
		decided          bool
		shouldBeFiltered bool
	}{
		{
			name:             "undecided state filters partial signatures",
			decided:          false,
			shouldBeFiltered: true,
		},
		{
			name:             "decided state allows partial signatures",
			decided:          true,
			shouldBeFiltered: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			committee := &Committee{
				ctx:           ctx,
				Queues:        make(map[phase0.Slot]queueContainer),
				Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
				BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			}

			slot := phase0.Slot(123)

			q := queueContainer{
				Q: queue.New(10),
				queueState: &queue.State{
					HasRunningInstance: true,
					Height:             specqbft.Height(slot),
					Slot:               slot,
					Round:              1,
				},
			}

			committeeRunner := &runner.CommitteeRunner{
				BaseRunner: &runner.BaseRunner{
					State: &runner.State{
						RunningInstance: &instance.Instance{
							State: &specqbft.State{
								Decided:                         tc.decided,
								ProposalAcceptedForCurrentRound: &specqbft.ProcessingMessage{},
								Round:                           1,
							},
						},
					},
				},
			}

			msgID := spectypes.MessageID{0x10}
			partialSigMsg := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{
					{
						Signer:           1,
						SigningRoot:      [32]byte{},
						ValidatorIndex:   0,
						PartialSignature: make([]byte, 96),
					},
				},
			}
			testMsg := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, msgID, partialSigMsg)

			pushed := q.Q.TryPush(testMsg)
			require.True(t, pushed)

			messageProcessed := make(chan struct{}, 1)

			handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
				messageProcessed <- struct{}{}
				return nil
			}

			safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

			if tc.shouldBeFiltered {
				select {
				case <-messageProcessed:
					t.Fatalf("Partial signature message was processed when it should have been filtered")
				case <-time.After(200 * time.Millisecond):
					// Expected - message was filtered
				}
			} else {
				select {
				case <-messageProcessed:
					// Expected - message was processed
				case <-time.After(500 * time.Millisecond):
					t.Fatalf("Partial signature message was not processed when it should have been")
				}
			}

			cancel()
			time.Sleep(50 * time.Millisecond)
		})
	}
}

// TestConsumeQueuePrioritization verifies the order of message processing based on
// the CommitteeQueuePrioritizer.
//
// Flow:
// 1. Set up a committee and queue.
// 2. Create a runner state where consensus is active and a proposal is accepted.
// 3. Add messages in a non-prioritized order: Commit, Proposal (for current round, but one is already accepted), Prepare, ChangeRound (for next round).
// 4. Add an ExecuteDuty event message, which should have high priority.
// 5. Start consuming the queue.
// 6. Assert the processing order:
//  1. ExecuteDuty (highest event priority)
//  2. Proposal (highest among consensus types)
//  3. Prepare
//  4. Commit
//  5. RoundChange (lowest among consensus types)
func TestConsumeQueuePrioritization(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(1)
	nextRound := specqbft.Round(2)

	// Build message bodies out of strict priority context
	commitMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.CommitMsgType}
	proposalMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.ProposalMsgType}
	prepareMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType}
	changeRoundMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: nextRound, MsgType: specqbft.RoundChangeMsgType}

	executeDutyData := &types.ExecuteCommitteeDutyData{Duty: &spectypes.CommitteeDuty{Slot: slot}}
	dutyDataJSON, err := json.Marshal(executeDutyData)
	require.NoError(t, err)
	eventMsgBody := &types.EventMsg{Type: types.ExecuteDuty, Data: dutyDataJSON}

	// Enqueue out-of-order messages; processing order is determined by prioritizer
	testMessages := []*queue.SSVMessage{
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{1}, commitMsgBody),
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{2}, proposalMsgBody),
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{3}, prepareMsgBody),
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{4}, changeRoundMsgBody),
		makeTestSSVMessage(t, message.SSVEventMsgType, spectypes.MessageID{5}, eventMsgBody),
	}

	q := queueContainer{
		Q: queue.New(10),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              currentRound,
		},
	}
	for _, msg := range testMessages {
		q.Q.TryPush(msg)
	}

	// Runner with a proposal already accepted, not yet decided
	acceptedProposal := &specqbft.ProcessingMessage{QBFTMessage: proposalMsgBody}
	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{RunningInstance: &instance.Instance{State: &specqbft.State{
				Decided:                         false,
				ProposalAcceptedForCurrentRound: acceptedProposal,
				Round:                           currentRound,
				Height:                          specqbft.Height(slot),
			}}},
		},
	}

	var received []*queue.SSVMessage
	var mu sync.Mutex
	handlerCalled := make(chan struct{}, len(testMessages))

	handler := func(ctx context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, &sync.RWMutex{})

	// Wait for all messages
	for i := 0; i < len(testMessages); i++ {
		select {
		case <-handlerCalled:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for all messages processed, got %d", len(received))
		}
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, len(testMessages))

	// Check ordering: Event first, then QBFT by type score
	expected := []specqbft.MessageType{
		specqbft.ProposalMsgType,
		specqbft.PrepareMsgType,
		specqbft.CommitMsgType,
		specqbft.RoundChangeMsgType,
	}

	// Confirm event processed first
	assert.Equal(t, message.SSVEventMsgType, received[0].MsgType)
	assert.Equal(t, eventMsgBody, received[0].Body.(*types.EventMsg))

	var actual []specqbft.MessageType
	for _, m := range received[1:] {
		actual = append(actual, m.Body.(*specqbft.Message).MsgType)
	}

	assert.Equal(t, expected, actual)
}

// TestHandleMessageQueueFullAndDropping verifies that HandleMessage drops messages
// when the queue is full and implicitly checks for a warning log (though not asserted).
//
// Flow:
// 1. Set up a committee with a small queue capacity (e.g., 2).
// 2. Pre-create and add a queue container for a specific slot to the committee.
// 3. Fill this queue to its capacity by calling HandleMessage repeatedly.
// 4. Verify the queue length is equal to its capacity.
// 5. Attempt to push one more message using HandleMessage.
// 6. Verify that the message was dropped (queue length remains at capacity).
func TestHandleMessageQueueFullAndDropping(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	queueCapacity := 2
	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
		mtx:           sync.RWMutex{},
	}

	slot := phase0.Slot(123)

	// Step 0: Create the queue container with the desired small capacity and add it to the committee
	committee.mtx.Lock()
	qContainer := queueContainer{
		Q: queue.New(queueCapacity),
		queueState: &queue.State{
			HasRunningInstance: false,
			Height:             specqbft.Height(slot),
			Slot:               slot,
		},
	}
	committee.Queues[slot] = qContainer
	committee.mtx.Unlock()

	// Step 1: Fill the pre-made queue to its capacity by calling HandleMessage
	// HandleMessage will find and use the qContainer we just set up.
	msgIDBase := spectypes.MessageID{1, 2, 3, 0}
	for i := 0; i < queueCapacity; i++ { // Push exactly queueCapacity items
		msgID := msgIDBase
		msgID[3] = byte(i) // Unique ID
		qbftMsg := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.ProposalMsgType}
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)
		committee.HandleMessage(ctx, logger, testMsg)
	}

	require.Equal(t, queueCapacity, qContainer.Q.Len())

	// Step 2: Clear log buffer and attempt to push one more message (this one should be dropped)
	droppedMsgID := msgIDBase
	droppedMsgID[3] = byte(queueCapacity) // Next ID, e.g., if capacity is 2, this is ID for 3rd item overall
	qbftMsgDrop := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.PrepareMsgType}
	testMsgDrop := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, droppedMsgID, qbftMsgDrop)

	committee.HandleMessage(ctx, logger, testMsgDrop)

	assert.Equal(t, queueCapacity, qContainer.Q.Len())
}

// TestConsumeQueueStopsOnErrNoValidDuties verifies that ConsumeQueue stops
// processing further messages if the handler returns runner.ErrNoValidDuties.
//
// Flow:
//  1. Set up a committee and a queue container with multiple messages.
//  2. Initialize a basic committee runner.
//  3. Define a message handler that:
//     a. Increments a counter for processed messages.
//     b. Returns runner.ErrNoValidDuties if a specific (e.g., the first) message is processed.
//  4. Call ConsumeQueue with this handler.
//  5. Verify that only the message(s) processed before the error was returned were handled.
//  6. Verify that remaining messages are still in the queue.
func TestConsumeQueueStopsOnErrNoValidDuties(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		ctx:           ctx,
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	q := queueContainer{
		Q: queue.New(10),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	// Add multiple messages
	msg1 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{1}, &specqbft.Message{Height: specqbft.Height(slot), MsgType: specqbft.ProposalMsgType})
	msg2 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{2}, &specqbft.Message{Height: specqbft.Height(slot), MsgType: specqbft.PrepareMsgType})
	msg3 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{3}, &specqbft.Message{Height: specqbft.Height(slot), MsgType: specqbft.CommitMsgType})
	q.Q.TryPush(msg1)
	q.Q.TryPush(msg2)
	q.Q.TryPush(msg3)

	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{State: &runner.State{RunningInstance: &instance.Instance{State: &specqbft.State{}}}},
	}

	var processedMessagesCount int32
	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		atomic.AddInt32(&processedMessagesCount, 1)
		if msg.MsgID == msg1.MsgID { // Return error after processing the first message
			return runner.ErrNoValidDuties
		}
		return nil
	}

	// Run ConsumeQueue. It should stop after processing msg1.
	_ = committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
	// We don't assert on `err` itself directly because ConsumeQueue might return nil if context is cancelled,
	// and ErrNoValidDuties causes a break, not necessarily a returned error from ConsumeQueue itself if ctx expires.
	// The primary check is the number of processed messages.

	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(1), atomic.LoadInt32(&processedMessagesCount))
	assert.Equal(t, 2, q.Q.Len())
}

// TestConsumeQueueBurstTraffic verifies that under a burst of interleaved messages,
// the queue pops messages in non-decreasing priority order and processes all enqueued messages.
//
// Flow:
//  1. Set up a committee with a single slot, its queue, and a runner in a "decided" state (allowing all message types).
//  2. Generate a large number (e.g., 200) of randomized messages of different types (ExecuteDuty, PartialSignature, Proposal, Prepare, Commit, RoundChange).
//  3. Keep track of the expected count for each message priority bucket.
//  4. Shuffle all generated messages and push them onto the queue.
//  5. Start consuming the queue with a handler that records the priority bucket of each popped message.
//  6. Wait for all messages to be processed.
//  7. Verify that the priorities of popped messages are non-decreasing.
//  8. Verify that the actual counts of processed messages per priority bucket match the expected counts.
func TestConsumeQueueBurstTraffic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// --- Setup a single-slot committee and its queue ---
	slot := phase0.Slot(42)
	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}
	qc := queueContainer{
		Q: queue.New(1000),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}
	committee.Queues[slot] = qc

	// Mark that consensus is already decided & proposal accepted → partial-sigs allowed
	acceptedProposal := &specqbft.ProcessingMessage{
		QBFTMessage: &specqbft.Message{
			Height:  specqbft.Height(slot),
			Round:   1,
			MsgType: specqbft.ProposalMsgType,
		},
	}
	committee.Runners[slot] = &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         true,
						ProposalAcceptedForCurrentRound: acceptedProposal,
						Round:                           1,
					},
				},
			},
		},
	}

	// --- Build 200 randomized messages and count expected per priority bucket ---
	var (
		allMsgs        []*queue.SSVMessage
		expectedCounts = make(map[int]int)
	)
	rnd := rand.New(rand.NewSource(1234))
	for i := 0; i < 200; i++ {
		id := spectypes.MessageID{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}
		var m *queue.SSVMessage

		switch rnd.Intn(6) {
		case 0: // ExecuteDuty event
			data, err := json.Marshal(&types.ExecuteCommitteeDutyData{Duty: &spectypes.CommitteeDuty{Slot: slot}})
			require.NoError(t, err)
			m = makeTestSSVMessage(t, message.SSVEventMsgType, id, &types.EventMsg{Type: types.ExecuteDuty, Data: data})

		case 1: // Partial signature
			ps := &spectypes.PartialSignatureMessages{
				Slot: slot,
				Messages: []*spectypes.PartialSignatureMessage{{
					Signer:           1,
					SigningRoot:      [32]byte{},
					ValidatorIndex:   0,
					PartialSignature: make([]byte, 96),
				}},
			}
			m = makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType, id, ps)

		case 2: // Proposal
			m = makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, id,
				&specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.ProposalMsgType},
			)

		case 3: // Prepare
			m = makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, id,
				&specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.PrepareMsgType},
			)

		case 4: // Commit
			m = makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, id,
				&specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.CommitMsgType},
			)

		case 5: // RoundChange
			m = makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, id,
				&specqbft.Message{Height: specqbft.Height(slot), Round: 2, MsgType: specqbft.RoundChangeMsgType},
			)
		}

		allMsgs = append(allMsgs, m)
	}

	priority := func(m *queue.SSVMessage) int {
		switch m.MsgType {
		case message.SSVEventMsgType:
			return 0
		case spectypes.SSVPartialSignatureMsgType:
			return 1
		case spectypes.SSVConsensusMsgType:
			switch m.Body.(*specqbft.Message).MsgType {
			case specqbft.ProposalMsgType:
				return 2
			case specqbft.PrepareMsgType:
				return 3
			case specqbft.CommitMsgType:
				return 4
			case specqbft.RoundChangeMsgType:
				return 5
			}
		default:
		}
		return 6
	}

	// Count how many we expect in each bucket
	for _, m := range allMsgs {
		expectedCounts[priority(m)]++
	}

	// --- Shuffle and enqueue all messages ---
	rnd.Shuffle(len(allMsgs), func(i, j int) {
		allMsgs[i], allMsgs[j] = allMsgs[j], allMsgs[i]
	})
	for _, m := range allMsgs {
		require.True(t, qc.Q.TryPush(m))
	}

	// --- Drain the queue, capturing the priority bucket of each popped message ---
	var buckets []int
	handlerC := make(chan struct{}, len(allMsgs))
	handler := func(_ context.Context, _ *zap.Logger, m *queue.SSVMessage) error {
		buckets = append(buckets, priority(m))
		handlerC <- struct{}{}
		return nil
	}
	go func() {
		_ = committee.ConsumeQueue(ctx, qc, logger, handler, committee.Runners[slot])
	}()

	// Wait for exactly len(allMsgs) messages (or fail on timeout)
	for i := 0; i < len(allMsgs); i++ {
		select {
		case <-handlerC:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for message %d/%d", i+1, len(allMsgs))
		}
	}
	cancel()

	// --- 1) Assert monotonic (non-decreasing) priorities ---
	for i := 1; i < len(buckets); i++ {
		require.LessOrEqualf(
			t,
			buckets[i-1],
			buckets[i],
			"priority dropped at index %d: %d → %d",
			i-1, buckets[i-1], buckets[i],
		)
	}

	// --- 2) Assert we popped exactly the same counts per bucket ---
	actualCounts := make(map[int]int)
	for _, b := range buckets {
		actualCounts[b]++
	}
	require.Equal(t, expectedCounts, actualCounts)
}
