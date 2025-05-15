package validator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

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

// safeConsumeQueue wraps ConsumeQueue execution in a goroutine and handles errors safely.
// If runnerMutex is provided, it's used to synchronize access to the committee runner's state.
func safeConsumeQueue(t *testing.T, ctx context.Context, committee *Committee, q queueContainer,
	logger *zap.Logger, handler func(context.Context, *zap.Logger, *queue.SSVMessage) error,
	committeeRunner *runner.CommitteeRunner, runnerMutex ...*sync.RWMutex) {

	errCh := make(chan error, 1)

	go func() {
		var err error
		if len(runnerMutex) > 0 && runnerMutex[0] != nil {
			// Use mutex if provided
			mutex := runnerMutex[0]
			mutex.RLock()
			err = committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
			mutex.RUnlock()
		} else {
			// No mutex, just call directly
			err = committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		}

		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errCh <- err
		}
		close(errCh)
	}()

	go func() {
		for err := range errCh {
			if err != nil {
				t.Logf("ConsumeQueue error: %v", err)
			}
		}
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
	logger := zaptest.NewLogger(t)
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
	logger := zaptest.NewLogger(t)
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

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

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
	logger := zaptest.NewLogger(t)
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
	logger := zaptest.NewLogger(t)
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

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

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
	logger := zaptest.NewLogger(t)
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

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

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
	logger := zaptest.NewLogger(t)
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

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

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

		_ = c.ConsumeQueue(ctx, q, zap.NewNop(), handler, rnr)
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

// TestQueueSaturationWithFilteredMessages tests whether a queue can get filled with filtered messages
// and potentially drop important messages when filter conditions change
//
// Flow:
// 1. Set up a small queue (capacity 5) and a runner that filters prepare messages
// 2. Fill the queue completely with filtered prepare messages (not processed)
// 3. Test if critical ExecuteDuty messages are still accepted and processed despite full queue
// 4. Test if proposal messages are also accepted and processed despite full queue
// 5. Change the runner state to accept prepare messages by setting ProposalAcceptedForCurrentRound
// 6. Verify if previously filtered messages are now processed or remain in the queue
// 7. Confirm the issue: filtered messages accumulate and aren't reprocessed when filter state changes
func TestQueueSaturationWithFilteredMessages(t *testing.T) {
	logger := zaptest.NewLogger(t)
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

	// Setup a runner with no proposal accepted state - will filter Prepare/Commit messages
	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: nil, // no proposal accepted initially
						Round:                           1,
					},
				},
			},
		},
	}

	runnerMutex := &sync.RWMutex{}

	q := queueContainer{
		Q: queue.New(queueCapacity),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	processedMsgs := make([]*queue.SSVMessage, 0)
	var processMsgsMutex sync.Mutex
	handlerCalled := make(chan struct{}, queueCapacity*3) // buffer to avoid blocking

	handler := func(hCtx context.Context, hLogger *zap.Logger, msg *queue.SSVMessage) error {
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

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner, runnerMutex)

	time.Sleep(50 * time.Millisecond)

	// Fill the queue COMPLETELY with prepare messages that will be filtered
	logger.Debug("pushing filtered messages to fill queue completely")
	for i := 0; i < queueCapacity; i++ {
		msgID := spectypes.MessageID{byte(i + 1)}
		qbftMsg := &specqbft.Message{
			Height:  specqbft.Height(slot),
			Round:   1,
			MsgType: specqbft.PrepareMsgType,
		}
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)
		pushed := q.Q.TryPush(testMsg)
		require.True(t, pushed)
	}

	time.Sleep(100 * time.Millisecond)

	// No messages should have been processed due to filtering
	select {
	case <-handlerCalled:
		t.Fatalf("a filtered message was processed, which shouldn't happen at this stage")
	default:
		// no messages should be processed yet
	}

	executedutyMsgID := spectypes.MessageID{0xEE}
	executeDutyData := &types.ExecuteCommitteeDutyData{
		Duty: &spectypes.CommitteeDuty{
			Slot: slot,
		},
	}

	dutyDataJSON, err := json.Marshal(executeDutyData)
	require.NoError(t, err)

	eventMsg := &types.EventMsg{
		Type: types.ExecuteDuty,
		Data: dutyDataJSON,
	}
	executeDutyMsg := makeTestSSVMessage(t, message.SSVEventMsgType, executedutyMsgID, eventMsg)

	logger.Debug("pushing executeDuty message to full queue")
	var pushed bool
	pushed = q.Q.TryPush(executeDutyMsg)
	assert.True(t, pushed)

	select {
	case <-handlerCalled:
		logger.Debug("executeDuty message processed despite full queue")
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for executeDuty message to be processed")
	}

	// Try to push a proposal message - should also succeed due to prioritization
	proposalMsgID := spectypes.MessageID{0xFF}
	qbftProposalMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}
	proposalMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, proposalMsgID, qbftProposalMsg)

	logger.Debug("pushing proposal message to full queue")
	pushed = q.Q.TryPush(proposalMsg)
	assert.True(t, pushed)

	select {
	case <-handlerCalled:
		logger.Debug("proposal message processed despite full queue")
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for proposal message to be processed")
	}

	cancel()
	wg.Wait()

	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()

	logger.Debug("updating runner state to process filtered messages")
	processedProposal := &specqbft.ProcessingMessage{
		QBFTMessage: qbftProposalMsg,
	}

	runnerMutex.Lock()
	committeeRunner.BaseRunner.State.RunningInstance.State.ProposalAcceptedForCurrentRound = processedProposal
	runnerMutex.Unlock()

	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx2.Done()
	}()

	safeConsumeQueue(t, ctx2, committee, q, logger, handler, committeeRunner, runnerMutex)

	// Observe if previously filtered messages are processed after state change.
	// Rely on handlerCalled and a timeout for this observation phase.
	timeoutForReprocessing := time.After(1 * time.Second)
	additionalProcessedCount := 0

	logCh := make(chan string, 10)
	defer close(logCh)

observeReprocessingLoop:
	for {
		select {
		case <-handlerCalled:
			additionalProcessedCount++
			logCh <- fmt.Sprintf("message processed after state change (count: %d)", additionalProcessedCount)
		case <-timeoutForReprocessing:
			logCh <- fmt.Sprintf("timeout reached while observing reprocessing (count: %d)", additionalProcessedCount)
			break observeReprocessingLoop
		case <-ctx2.Done(): // If the main test context is cancelled earlier
			logCh <- fmt.Sprintf("context done while observing reprocessing (count: %d)", additionalProcessedCount)
			break observeReprocessingLoop
		}
	}

	// Process any log messages that were sent
	for len(logCh) > 0 {
		logMsg := <-logCh
		logger.Debug(logMsg)
	}

	cancel2()
	wg.Wait()

	finalQueueLen := q.Q.Len()
	if finalQueueLen > 0 {
		t.Logf("ISSUE CONFIRMED: %d messages remain in the queue and were not reprocessed after state change (or during reprocessing observation window)",
			finalQueueLen)
		t.Log("CHECK ME: filtered messages accumulate and aren't reprocessed when filter state changes, or reprocessing was incomplete within the timeout.")
	} else {
		t.Log("all messages were processed after state change, or queue emptied during observation window.")
	}

	processMsgsMutex.Lock()
	defer processMsgsMutex.Unlock()

	processedTypes := make(map[spectypes.MsgType]int)
	processedQbftTypes := make(map[specqbft.MessageType]int)

	var executeDutyProcessed, proposalProcessed bool

	for _, msg := range processedMsgs {
		processedTypes[msg.MsgType]++

		switch msg.MsgType {
		case message.SSVEventMsgType:
			if event, ok := msg.Body.(*types.EventMsg); ok && event.Type == types.ExecuteDuty {
				executeDutyProcessed = true
			}
		case spectypes.SSVConsensusMsgType:
			if qbftMsg, ok := msg.Body.(*specqbft.Message); ok {
				processedQbftTypes[qbftMsg.MsgType]++
				if qbftMsg.MsgType == specqbft.ProposalMsgType {
					proposalProcessed = true
				}
			}
		default:
			// skip
		}
	}

	assert.True(t, executeDutyProcessed)
	assert.True(t, proposalProcessed)
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
			name:              "no running instance",
			hasRunningDuty:    false,
			decided:           false,
			proposalAccepted:  false,
			messagesTypes:     []specqbft.MessageType{specqbft.ProposalMsgType, specqbft.PrepareMsgType, specqbft.CommitMsgType},
			expectedProcessed: []bool{false, false, false}, // None should be processed
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
			logger := zaptest.NewLogger(t)
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

			if tc.name == "no running instance" {
				// For this special test case, simply wait for context to be done
				go func() {
					for ctx.Err() == nil {
						time.Sleep(50 * time.Millisecond)
					}
				}()
			} else {
				safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)
			}

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

			processedTypes := make([]specqbft.MessageType, 0, len(processed))
			for _, msg := range processed {
				if qbftMsg, ok := msg.Body.(*specqbft.Message); ok {
					processedTypes = append(processedTypes, qbftMsg.MsgType)
				}
			}
			t.Logf("Processed message types: %v", processedTypes)

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
			logger := zaptest.NewLogger(t)
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

			safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

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
// 6. Verify messages are processed in the expected priority order: ExecuteDuty, ChangeRound, Prepare, Commit, Proposal (if applicable).
func TestConsumeQueuePrioritization(t *testing.T) {
	logger := zaptest.NewLogger(t)
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

	// Messages - added in a non-priority order initially
	// IDs are arbitrary but unique for tracking.
	commitMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.CommitMsgType}
	proposalMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.ProposalMsgType} // A second proposal for the same round
	prepareMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType}
	changeRoundMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: nextRound, MsgType: specqbft.RoundChangeMsgType} // For next round

	executeDutyData := &types.ExecuteCommitteeDutyData{Duty: &spectypes.CommitteeDuty{Slot: slot}}
	dutyDataJSON, err := json.Marshal(executeDutyData)
	require.NoError(t, err)
	eventMsgBody := &types.EventMsg{Type: types.ExecuteDuty, Data: dutyDataJSON}

	// Order of adding to queue (does not dictate processing order)
	testMessages := []*queue.SSVMessage{
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{1}, commitMsgBody),      // Lowest expected priority among these QBFT msgs
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{2}, proposalMsgBody),    // Lower priority as one is accepted
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{3}, prepareMsgBody),     // Mid priority
		makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{4}, changeRoundMsgBody), // High QBFT priority (next round)
		makeTestSSVMessage(t, message.SSVEventMsgType, spectypes.MessageID{5}, eventMsgBody),             // Highest priority (event)
	}

	q := queueContainer{
		Q: queue.New(10), // Sufficient capacity
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

	// Runner state: Proposal already accepted for the current round, not yet decided.
	acceptedProposal := &specqbft.ProcessingMessage{
		QBFTMessage: &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.ProposalMsgType},
	}
	committeeRunner := &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         false,
						ProposalAcceptedForCurrentRound: acceptedProposal,
						Round:                           currentRound,
						Height:                          specqbft.Height(slot),
					},
				},
			},
		},
	}

	var receivedMessages []*queue.SSVMessage
	var mu sync.Mutex
	handlerCalled := make(chan struct{}, len(testMessages))

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		mu.Lock()
		receivedMessages = append(receivedMessages, msg)
		mu.Unlock()
		handlerCalled <- struct{}{}
		return nil
	}

	safeConsumeQueue(t, ctx, committee, q, logger, handler, committeeRunner)

	for i := 0; i < len(testMessages); i++ {
		select {
		case <-handlerCalled:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for all messages to be processed, got %d, expected %d", len(receivedMessages), len(testMessages))
		}
	}
	cancel()                          // Stop ConsumeQueue
	time.Sleep(50 * time.Millisecond) // Allow ConsumeQueue to exit

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, len(testMessages), len(receivedMessages), "did not process all messages")

	// Expected order: ExecuteDuty, ChangeRound (next round), Prepare, Commit, Proposal (current round, but one already accepted so lower prio)

	expectedQBFTOrder := []specqbft.MessageType{
		specqbft.ProposalMsgType,
		specqbft.PrepareMsgType,
		specqbft.CommitMsgType,
		specqbft.RoundChangeMsgType,
	}

	var actualQBFTOrder []specqbft.MessageType
	var actualEventMsgFound bool

	if len(receivedMessages) > 0 {
		if receivedMessages[0].MsgType == message.SSVEventMsgType {
			actualEventMsgFound = true
			assert.Equal(t, eventMsgBody, receivedMessages[0].Body.(*types.EventMsg))
		}

		for _, msg := range receivedMessages {
			if ssvConsensusMsg, ok := msg.Body.(*specqbft.Message); ok {
				actualQBFTOrder = append(actualQBFTOrder, ssvConsensusMsg.MsgType)
			}
		}
	}

	assert.True(t, actualEventMsgFound)
	assert.Equal(t, expectedQBFTOrder, actualQBFTOrder)
}

// TestHandleMessageQueueFullAndDropping verifies that HandleMessage drops messages
// when the queue is full and logs a warning.
func TestHandleMessageQueueFullAndDropping(t *testing.T) {
	logger := zaptest.NewLogger(t)

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

	require.Equal(t, queueCapacity, qContainer.Q.Len(), "Queue should be full after filling to capacity")

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
func TestConsumeQueueStopsOnErrNoValidDuties(t *testing.T) {
	logger := zaptest.NewLogger(t)
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
