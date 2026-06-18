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
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
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
func makeTestSSVMessage(t *testing.T, msgType spectypes.MsgType, msgID spectypes.MessageID, body any) *queue.SSVMessage {
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

// runConsumeQueueAsync wraps ConsumeQueue execution in a goroutine.
func runConsumeQueueAsync(
	t *testing.T,
	ctx context.Context,
	committee *Committee,
	q queue.Queue,
	logger *zap.Logger,
	handler MessageHandler,
	committeeRunner *runner.CommitteeRunner,
) {
	t.Helper()

	go func() {
		committee.ConsumeQueue(ctx, logger, queueContainer{Q: q}, handler, committeeRunner)
	}()
}

// collectMessagesFromQueue collects and returns messages received on the provided channel.
// It waits for the expected number of messages or times out.
func collectMessagesFromQueue(t *testing.T, msgChannel <-chan *queue.SSVMessage, expectedCount int, timeout time.Duration) []*queue.SSVMessage {
	t.Helper()

	var receivedMessages []*queue.SSVMessage

	// Special case: expectedCount == 0 means we're checking for absence of messages
	if expectedCount == 0 {
		select {
		case msg := <-msgChannel:
			t.Errorf("received unexpected message when expecting none: %v", msg)
			receivedMessages = append(receivedMessages, msg)
		case <-time.After(timeout):
			// This is the expected case - no messages received within timeout
		}
		return receivedMessages
	}

	// Wait for the expected number of messages
	for i := 0; i < expectedCount; i++ {
		select {
		case msg := <-msgChannel:
			receivedMessages = append(receivedMessages, msg)
		case <-time.After(timeout):
			t.Fatalf("timed out waiting for message %d/%d", i+1, expectedCount)
		}
	}

	// Check if we received more messages than expected (test issue indicator)
	select {
	case msg := <-msgChannel:
		t.Logf("received more messages than expected (%d)", expectedCount)
		receivedMessages = append(receivedMessages, msg)
	case <-time.After(400 * time.Millisecond):
		// No additional messages within timeout, which is the expected case
	}

	return receivedMessages
}

// setupMessageCollection creates a message channel and handler function for queue message processing.
// It returns the message channel and the handler function that adds messages to this channel.
func setupMessageCollection(capacity int) (chan *queue.SSVMessage, MessageHandler) {
	msgChannel := make(chan *queue.SSVMessage, max(1, capacity))

	handler := func(ctx context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		msgChannel <- msg
		return nil
	}

	return msgChannel, handler
}

func newCommitteeQueueStateForTest(slot phase0.Slot, round specqbft.Round, hasRunningInstance bool, quorum uint64) *queue.State {
	return &queue.State{
		HasRunningInstance: hasRunningInstance,
		Slot:               slot,
		Round:              round,
		Quorum:             quorum,
	}
}

// newCommitteeRunnerForTest builds a minimal CommitteeRunner whose getters expose just enough state for
// queue-consumer tests to exercise filtering and prioritization without spinning up the full runner stack.
//   - slot     → drives QBFTController.LatestInstanceHeight, so r.GetLastHeight() returns specqbft.Height(slot).
//   - round    → drives the running QBFT instance's Round, so r.GetLastRound() returns it.
//   - decided  → marks the running instance as decided (used by the "skip PSM until decided" filter).
//   - proposal → if non-nil, sets ProposalAcceptedForCurrentRound (used by HasAcceptedProposalForCurrentRound).
func newCommitteeRunnerForTest(
	slot phase0.Slot,
	round specqbft.Round,
	decided bool,
	proposal *specqbft.ProcessingMessage,
) *runner.CommitteeRunner {
	return &runner.CommitteeRunner{
		BaseRunner: &runner.BaseRunner{
			QBFTController: &controller.Controller{
				LatestInstanceHeight: specqbft.Height(slot),
			},
			State: &runner.State{
				RunningInstance: &instance.Instance{
					State: &specqbft.State{
						Decided:                         decided,
						ProposalAcceptedForCurrentRound: proposal,
						Round:                           round,
					},
				},
			},
		},
	}
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := log.TestLogger(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	slot := phase0.Slot(123)

	committee := &Committee{
		logger:            logger,
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
	}

	msgID := spectypes.MessageID{1, 2, 3, 4}
	qbftMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   1,
		MsgType: specqbft.ProposalMsgType,
	}
	testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)

	committee.EnqueueMessage(ctx, testMsg)

	q, ok := committee.Queues[slot]

	require.True(t, ok)

	assert.NotNil(t, q.Q)
	assert.Equal(t, 1, q.Q.Len())

	queuedMsg := q.Q.TryPop(
		queue.NewCommitteeQueuePrioritizer(
			newCommitteeQueueStateForTest(slot, 0, false, committee.CommitteeMember.GetQuorum()),
		),
		queue.FilterAny,
	)
	require.NotNil(t, queuedMsg)
	assert.Equal(t, testMsg.MsgID, queuedMsg.MsgID)
	assert.Equal(t, testMsg.MsgType, queuedMsg.MsgType)
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := log.TestLogger(t)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		logger:            logger,
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
	}

	slot := phase0.Slot(123)
	msgID1 := spectypes.MessageID{1, 2, 3, 4} // Proposal message, round 1
	msgID2 := spectypes.MessageID{5, 6, 7, 8} // Prepare message, round 1

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

	q := queue.New(logger, 1000)
	q.TryPush(testMsg1)
	q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg1,
	}

	committeeRunner := newCommitteeRunnerForTest(slot, 1, false, proposalMsg)

	msgChannel, handler := setupMessageCollection(2)
	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	receivedMessages := collectMessagesFromQueue(t, msgChannel, 2, 1*time.Second)

	assert.Equal(t, 2, len(receivedMessages))
	assert.Equal(t, specqbft.ProposalMsgType, receivedMessages[0].Body.(*specqbft.Message).MsgType)
	assert.Equal(t, specqbft.PrepareMsgType, receivedMessages[1].Body.(*specqbft.Message).MsgType)
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
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
	}

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(1)
	nextRound := specqbft.Round(2)

	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

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

	// Combine message IDs and QBFT messages for shuffling
	type messageWithID struct {
		ID      spectypes.MessageID
		Message *specqbft.Message
	}

	combinedMessages := make([]messageWithID, len(msgIDs))
	for i := range msgIDs {
		combinedMessages[i] = messageWithID{ID: msgIDs[i], Message: qbftMessages[i]}
	}

	rnd.Shuffle(len(combinedMessages), func(i, j int) {
		combinedMessages[i], combinedMessages[j] = combinedMessages[j], combinedMessages[i]
	})

	q := queue.New(logger, 1000)

	for _, combined := range combinedMessages {
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, combined.ID, combined.Message)
		q.TryPush(testMsg)
	}

	committeeRunner := newCommitteeRunnerForTest(slot, currentRound, false, nil)

	msgChannel, handler := setupMessageCollection(4)
	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	receivedMessages := collectMessagesFromQueue(t, msgChannel, 4, 1*time.Second)

	require.Equal(t, 4, len(receivedMessages))

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
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
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

	q := queue.New(logger, 1000)

	q.TryPush(testMsg1)
	q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg,
	}

	committeeRunner := newCommitteeRunnerForTest(slot, 1, false, proposalMsg)

	msgChannel, handler := setupMessageCollection(2)
	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	receivedMessages := collectMessagesFromQueue(t, msgChannel, 1, 1*time.Second)

	require.Equal(t, 1, len(receivedMessages))
	assert.Equal(t, spectypes.SSVConsensusMsgType, receivedMessages[0].MsgType)
}

// TestFilterDecidedAllowsAll verifies that all message types are processed
// when the consensus instance has reached a decision.
func TestFilterDecidedAllowsAll(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
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

	q := queue.New(logger, 1000)

	q.TryPush(testMsg1)
	q.TryPush(testMsg2)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftMsg,
	}

	committeeRunner := newCommitteeRunnerForTest(slot, 1, true, proposalMsg)

	msgChannel, handler := setupMessageCollection(2)
	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	receivedMessages := collectMessagesFromQueue(t, msgChannel, 2, 1*time.Second)

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
	logger := log.TestLogger(t)

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
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		var seen *queue.SSVMessage
		handler := func(_ context.Context, _ *zap.Logger, m *queue.SSVMessage) error {
			seen = m
			// Return error to make ConsumeQueue exit early after processing one message.
			// This is intentional for this test which just needs to check if a message was filtered.
			return fmt.Errorf("intentionally stopping ConsumeQueue after first message")
		}

		q := queue.New(logger, 1)
		q.TryPush(prepareMsg)

		c := &Committee{
			networkConfig:   networkconfig.TestNetwork,
			CommitteeMember: &spectypes.CommitteeMember{},
		}
		c.ConsumeQueue(ctx, logger, queueContainer{Q: q}, handler, rnr)
		return seen
	}

	// 1) No proposal accepted => Prepare should be filtered out
	r1 := newCommitteeRunnerForTest(slot, round, false, nil)
	seen1 := runOnce(r1)
	assert.Nil(t, seen1)

	// 2) Proposal accepted => now we should see exactly one Prepare
	r2 := newCommitteeRunnerForTest(slot, round, false, &specqbft.ProcessingMessage{QBFTMessage: prepareBody})
	seen2 := runOnce(r2)

	require.NotNil(t, seen2)
	assert.Equal(t, specqbft.PrepareMsgType, seen2.Body.(*specqbft.Message).MsgType)
}

// TestFilterUsesCurrentRunnerRound is a regression test for a bug where the committee
// queue consumer's state.Round was never updated from its default zero, so the
// "skip prepare/commit for current round when no proposal accepted" filter only
// worked at round 0 (i.e. never, in practice — QBFT starts at round 1).
//
// The runner is placed at round 3 with no proposal accepted for the current round.
// A prepare message at round 3 must be filtered out; a prepare message at round 2
// (a stale/lower round) must pass the filter.
func TestFilterUsesCurrentRunnerRound(t *testing.T) {
	logger := log.TestLogger(t)

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(3) // past round 1 — exercises the Round-update fix

	runOnce := func(msg *queue.SSVMessage) *queue.SSVMessage {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		q := queue.New(logger, 1)
		q.TryPush(msg)

		var seen *queue.SSVMessage
		handler := func(_ context.Context, _ *zap.Logger, m *queue.SSVMessage) error {
			seen = m
			// Return error to make ConsumeQueue exit early after processing one message.
			return fmt.Errorf("intentionally stopping ConsumeQueue after first message")
		}

		c := &Committee{
			networkConfig:   networkconfig.TestNetwork,
			CommitteeMember: &spectypes.CommitteeMember{},
		}
		r := newCommitteeRunnerForTest(slot, currentRound, false, nil)
		c.ConsumeQueue(ctx, logger, queueContainer{Q: q}, handler, r)
		return seen
	}

	// 1) Prepare at the runner's current round (3) — must be filtered out.
	prepareAtCurrentRound := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType,
		spectypes.MessageID{1},
		&specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType},
	)
	assert.Nil(t, runOnce(prepareAtCurrentRound), "prepare at current round should be filtered out")

	// 2) Prepare at a different (earlier) round — must pass the filter.
	prepareAtOtherRound := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType,
		spectypes.MessageID{2},
		&specqbft.Message{Height: specqbft.Height(slot), Round: currentRound - 1, MsgType: specqbft.PrepareMsgType},
	)
	seen := runOnce(prepareAtOtherRound)
	require.NotNil(t, seen, "prepare at a different round should not be filtered")
	assert.Equal(t, specqbft.PrepareMsgType, seen.Body.(*specqbft.Message).MsgType)
}

// TestPostConsensusOutranksConsensusAtRunnerSlot is a regression test that pins the
// committee prioritiser invariant "PostConsensus PSM outranks same-slot consensus message"
// while driving rState construction through ConsumeQueue. Historically the priority arithmetic
// ranked QBFT above PSM whenever rState's slot/height fields disagreed about the runner's
// current slot — see queue.compareHeightOrSlot — so the test pushes one of each and asserts
// the PSM is delivered first.
func TestPostConsensusOutranksConsensusAtRunnerSlot(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	slot := phase0.Slot(100)

	committee := &Committee{
		networkConfig:   networkconfig.TestNetwork,
		Queues:          make(map[phase0.Slot]queueContainer),
		Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
		CommitteeMember: &spectypes.CommitteeMember{},
	}

	// Decided runner: HasRunningQBFTInstance() returns false so ConsumeQueue applies no
	// PSM-blocking filter. The order observed by the handler is therefore set entirely by
	// the prioritiser — which is exactly what we want to assert here.
	committeeRunner := newCommitteeRunnerForTest(slot, 1, true, &specqbft.ProcessingMessage{
		QBFTMessage: &specqbft.Message{},
	})

	qbftMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType,
		spectypes.MessageID{1},
		&specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.CommitMsgType},
	)
	psmMsg := makeTestSSVMessage(t, spectypes.SSVPartialSignatureMsgType,
		spectypes.MessageID{2},
		&spectypes.PartialSignatureMessages{
			Slot: slot,
			Type: spectypes.PostConsensusPartialSig,
			Messages: []*spectypes.PartialSignatureMessage{{
				Signer:           1,
				SigningRoot:      [32]byte{},
				ValidatorIndex:   0,
				PartialSignature: make([]byte, 96),
			}},
		},
	)

	q := queue.New(logger, 10)
	require.True(t, q.TryPush(qbftMsg))
	require.True(t, q.TryPush(psmMsg))

	msgChannel, handler := setupMessageCollection(2)
	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	received := collectMessagesFromQueue(t, msgChannel, 2, 1*time.Second)
	require.Len(t, received, 2)
	assert.Equal(t, spectypes.SSVPartialSignatureMsgType, received[0].MsgType,
		"PSM must be popped before same-slot consensus message")
	assert.Equal(t, spectypes.SSVConsensusMsgType, received[1].MsgType)
}

// TestCommitteeQueueFilteringScenarios verifies the message filtering logic of the ConsumeQueue method
// across a range of committee runner states. It ensures that SSV messages are appropriately
// processed or filtered based on the runner's current operational phase, such as whether a duty
// is active, a proposal has been accepted for the current round, or a consensus decision
// has been reached for the slot.
//
// The test defines a set of distinct scenarios, each configuring the committee runner
// to a specific state (e.g., "no active duty", "no proposal accepted", "proposal accepted but not decided", "decided").
// For each scenario, a collection of SSV messages with varying QBFT message types are pushed to the queue.
// The test then asserts that:
//   - Only those messages that align with the ConsumeQueue's filtering criteria for the given runner state
//     are passed to the message handler.
//   - Messages that should be filtered out in that state are indeed not processed.
//   - The count of processed messages matches the expected number for that scenario.
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
			logger := log.TestLogger(t)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			committee := &Committee{
				networkConfig:   networkconfig.TestNetwork,
				Queues:          make(map[phase0.Slot]queueContainer),
				Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
				CommitteeMember: &spectypes.CommitteeMember{},
			}

			slot := phase0.Slot(123)

			q := queue.New(logger, 10)

			var proposalMsg *specqbft.ProcessingMessage
			if tc.proposalAccepted {
				proposalMsg = &specqbft.ProcessingMessage{
					QBFTMessage: &specqbft.Message{},
				}
			}

			committeeRunner := newCommitteeRunnerForTest(slot, 1, tc.decided, proposalMsg)

			// Set runner state based on hasRunningDuty parameter
			if !tc.hasRunningDuty {
				committeeRunner.State.Succeeded = true // This makes hasDutyRunning() return false
			}

			msgChannel := make(chan *queue.SSVMessage, len(tc.messagesTypes))

			handler := func(ctx context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
				msgChannel <- msg
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
				pushed := q.TryPush(testMsg)
				require.True(t, pushed)
			}

			runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

			expectedProcessedCount := 0
			for _, shouldProcess := range tc.expectedProcessed {
				if shouldProcess {
					expectedProcessedCount++
				}
			}

			var processed []*queue.SSVMessage
			processedCount := 0

			if expectedProcessedCount > 0 {
				timeout := time.After(1 * time.Second)
			waitLoop:
				for processedCount < expectedProcessedCount {
					select {
					case msg := <-msgChannel:
						processed = append(processed, msg)
						processedCount++
					case <-timeout:
						break waitLoop
					}
				}
			} else {
				time.Sleep(400 * time.Millisecond)
			}

			// Verify no additional messages are processed
			if expectedProcessedCount > 0 {
				select {
				case msg := <-msgChannel:
					t.Fatalf("unexpected message processed (type: %v), expected only %d",
						msg.Body.(*specqbft.Message).MsgType, expectedProcessedCount)
				case <-time.After(400 * time.Millisecond):
					// timeout reached - no more messages, as expected
				}
			} else {
				// If we expect zero messages, just wait a bit longer to confirm none are processed
				select {
				case msg := <-msgChannel:
					t.Fatalf("unexpected message processed when expecting none: %v",
						msg.Body.(*specqbft.Message).MsgType)
				case <-time.After(400 * time.Millisecond):
					// Timeout reached - no messages, as expected
				}
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
			logger := log.TestLogger(t)
			ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
			defer cancel()

			committee := &Committee{
				networkConfig:   networkconfig.TestNetwork,
				Queues:          make(map[phase0.Slot]queueContainer),
				Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
				CommitteeMember: &spectypes.CommitteeMember{},
			}

			slot := phase0.Slot(123)

			q := queue.New(logger, 10)

			committeeRunner := newCommitteeRunnerForTest(slot, 1, tc.decided, &specqbft.ProcessingMessage{})

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

			pushed := q.TryPush(testMsg)
			require.True(t, pushed)

			if tc.shouldBeFiltered {
				// Expect 0 messages - message should be filtered
				msgChannel, handler := setupMessageCollection(1)
				runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)
				receivedMessages := collectMessagesFromQueue(t, msgChannel, 0, 200*time.Millisecond)
				assert.Empty(t, receivedMessages)
			} else {
				// Expect 1 message - message should be processed
				msgChannel, handler := setupMessageCollection(1)
				runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)
				receivedMessages := collectMessagesFromQueue(t, msgChannel, 1, 1*time.Second)
				assert.Len(t, receivedMessages, 1)
			}
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
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	committee := &Committee{
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
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

	q := queue.New(logger, 10)
	for _, msg := range testMessages {
		q.TryPush(msg)
	}

	// Runner with a proposal already accepted, not yet decided
	acceptedProposal := &specqbft.ProcessingMessage{QBFTMessage: proposalMsgBody}
	committeeRunner := newCommitteeRunnerForTest(slot, currentRound, false, acceptedProposal)

	msgChannel := make(chan *queue.SSVMessage, len(testMessages))

	handler := func(ctx context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		msgChannel <- msg
		return nil
	}

	runConsumeQueueAsync(t, ctx, committee, q, logger, handler, committeeRunner)

	var received []*queue.SSVMessage

	// Wait for all messages
	for i := 0; i < len(testMessages); i++ {
		select {
		case msg := <-msgChannel:
			received = append(received, msg)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for all messages processed, got %d", len(received))
		}
	}

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

	actual := make([]specqbft.MessageType, 0, len(received[1:]))
	for _, m := range received[1:] {
		actual = append(actual, m.Body.(*specqbft.Message).MsgType)
	}

	assert.Equal(t, expected, actual)
}

// TestHandleMessageQueueFullAndDropping verifies that HandleMessage drops messages
// when the queue is full. It also confirms that the correct message is dropped (the newest one)
// and that messages already in the queue remain untouched.
//
// Flow:
//  1. Set up a committee with a small queue capacity (e.g., 2).
//  2. Pre-create and add a queue container for a specific slot to the committee.
//  3. Fill this queue to its capacity by calling HandleMessage repeatedly with initial messages.
//  4. Verify the queue length is equal to its capacity.
//  5. Attempt to push one more message (the "dropped message") using HandleMessage.
//  6. Verify that the queue length remains at capacity (indicating a message was dropped).
//  7. Pop all messages from the queue and verify:
//     a. The "dropped message" is NOT present among the popped messages.
//     b. All initial messages ARE present and are of the correct type.
//  8. Verify the queue is empty after all messages have been popped.
func TestHandleMessageQueueFullAndDropping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := log.TestLogger(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	slot := phase0.Slot(123)

	queueCapacity := 2
	committee := &Committee{
		logger:          logger,
		networkConfig:   networkconfig.TestNetwork,
		Queues:          make(map[phase0.Slot]queueContainer),
		CommitteeMember: &spectypes.CommitteeMember{},
	}

	// Step 0: Create the queue container with the desired small capacity and add it to the committee
	qContainer := queue.New(logger, queueCapacity)
	qState := newCommitteeQueueStateForTest(slot, 0, false, committee.CommitteeMember.GetQuorum())
	committee.Queues[slot] = queueContainer{Q: qContainer}

	// Step 1: Fill the pre-made queue to its capacity by calling HandleMessage
	// HandleMessage will find and use the qContainer we just set up.
	msgIDBase := spectypes.MessageID{1, 2, 3, 0}
	for i := range queueCapacity { // Push exactly queueCapacity items
		msgID := msgIDBase
		msgID[3] = byte(i) // Unique ID
		qbftMsg := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.ProposalMsgType}
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)
		committee.EnqueueMessage(ctx, testMsg)
	}

	require.Equal(t, queueCapacity, qContainer.Len())

	// Step 2: Clear log buffer and attempt to push one more message (this one should be dropped)
	droppedMsgID := msgIDBase
	droppedMsgID[3] = byte(queueCapacity) // Next ID, e.g., if capacity is 2, this is ID for 3rd item overall
	qbftMsgDrop := &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.PrepareMsgType}
	testMsgDrop := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, droppedMsgID, qbftMsgDrop)

	committee.EnqueueMessage(ctx, testMsgDrop)

	assert.Equal(t, queueCapacity, qContainer.Len())

	// Step 3: Verify that the dropped message is not in the queue and original messages are intact.
	// Pop messages one by one and check their MsgID and Type.
	// The `testMsgDrop` (Prepare message) should not be found.
	// All `queueCapacity` messages found should be Proposal messages from Step 1.
	foundDroppedMessage := false
	proposalMessageCount := 0
	for range queueCapacity {
		popCtx, popCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		// Use FilterAny since we are just checking the contents, not a live consumption scenario.
		// The prioritizer does not matter here as we drain the queue completely.
		msg := qContainer.Pop(popCtx, queue.NewCommitteeQueuePrioritizer(qState), queue.FilterAny)
		popCancel()

		require.NotNil(t, msg)

		if msg.MsgID == droppedMsgID {
			foundDroppedMessage = true
		}

		if qbftBody, ok := msg.Body.(*specqbft.Message); ok {
			if qbftBody.MsgType == specqbft.ProposalMsgType {
				// Check if the MsgID matches one of the initially enqueued messages.
				initialMsgID := msgIDBase
				initialMsgID[3] = msg.MsgID[3] // original ID
				if msg.MsgID == initialMsgID && msg.MsgID[3] < byte(queueCapacity) {
					proposalMessageCount++
				}
			}
		} else {
			t.Errorf("unexpected message body type: %T", msg.Body)
		}
	}

	assert.False(t, foundDroppedMessage)
	assert.Equal(t, queueCapacity, proposalMessageCount)

	// Step 4: Verify the queue is now empty.
	finalPopCtx, finalPopCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer finalPopCancel()

	assert.Nil(t, qContainer.Pop(finalPopCtx, queue.NewCommitteeQueuePrioritizer(qState), queue.FilterAny))
}

// TestConsumeQueueStopsOnErrNoValidDuties verifies that ConsumeQueue stops
// processing further messages if the handler returns runner.ErrNoValidDutiesToExecute.
//
// Flow:
//  1. Set up a committee and a queue container with multiple messages.
//  2. Initialize a basic committee runner.
//  3. Define a message handler that:
//     a. Increments a counter for processed messages.
//     b. Returns runner.ErrNoValidDutiesToExecute if a specific (e.g., the first) message is processed.
//  4. Call ConsumeQueue with this handler.
//  5. Verify that only the message(s) processed before the error was returned were handled.
//  6. Verify that remaining messages are still in the queue.
func TestConsumeQueueStopsOnErrNoValidDuties(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	committee := &Committee{
		networkConfig:   networkconfig.TestNetwork,
		CommitteeMember: &spectypes.CommitteeMember{},
	}

	slot := phase0.Slot(123)
	q := queue.New(logger, 10)

	// Add multiple messages
	msg1 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{1}, &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.ProposalMsgType})
	msg2 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{2}, &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.PrepareMsgType})
	msg3 := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{3}, &specqbft.Message{Height: specqbft.Height(slot), Round: 1, MsgType: specqbft.CommitMsgType})
	q.TryPush(msg1)
	q.TryPush(msg2)
	q.TryPush(msg3)

	committeeRunner := newCommitteeRunnerForTest(slot, 1, false, nil)

	var processedMessagesCount int32
	handler := func(ctx context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
		atomic.AddInt32(&processedMessagesCount, 1)
		if msg.MsgID == msg1.MsgID { // Return error after processing the first message
			return runner.ErrNoValidDutiesToExecute
		}
		return nil
	}

	// Run ConsumeQueue. It should stop after processing the first message because the handler
	// returns runner.ErrNoValidDutiesToExecute.
	// The ConsumeQueue method itself is designed to break its processing loop and return nil
	// when its handler signals ErrNoValidDutiesToExecute, treating it as a normal stop condition for the queue.
	// Thus, we expect no error from the ConsumeQueue call.
	committee.ConsumeQueue(ctx, logger, queueContainer{Q: q}, handler, committeeRunner)

	assert.Equal(t, int32(1), atomic.LoadInt32(&processedMessagesCount))
	assert.Equal(t, 2, q.Len())
}

// TestConsumeQueueBurstTraffic verifies that under a burst of interleaved messages,
// the queue pops messages in a way that never emits a strictly-lower-priority message
// before a higher-priority one, and drains the entire queue.
//
// Flow:
//  1. Set up a single-slot committee and its queue.
//  2. Generate a large number (e.g., 200) of randomized messages of different types
//     (ExecuteDuty, PartialSignature, Proposal, Prepare, Commit, RoundChange) and
//     count the expected per-type totals.
//  3. Shuffle all generated messages and push them onto the queue.
//  4. Drain the queue via Pop using the committee prioritizer.
//  5. Verify that no popped message was ever strictly higher priority than the one
//     popped before it (monotonic non-decreasing priority).
//  6. Verify that the per-type counts of popped messages match the expected counts.
func TestConsumeQueueBurstTraffic(t *testing.T) {
	logger := log.TestLogger(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// --- Setup a single-slot committee and its queue ---
	slot := phase0.Slot(42)
	committee := &Committee{
		networkConfig:     networkconfig.TestNetwork,
		Queues:            make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		CommitteeMember:   &spectypes.CommitteeMember{},
	}
	qc := queue.New(logger, 1000)
	committee.Queues[slot] = queueContainer{Q: qc}

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
		require.True(t, qc.TryPush(m))
	}

	received := make([]*queue.SSVMessage, 0, len(allMsgs))
	prioritizer := queue.NewCommitteeQueuePrioritizer(
		newCommitteeQueueStateForTest(slot, 1, false, committee.CommitteeMember.GetQuorum()),
	)
	for i := 0; i < len(allMsgs); i++ {
		msg := qc.Pop(ctx, prioritizer, queue.FilterAny)
		require.NotNilf(t, msg, "timed out waiting for message %d/%d", i+1, len(allMsgs))
		received = append(received, msg)
	}

	// --- 1) Assert the queue never emitted a message that was lower priority than the next one ---
	for i := 1; i < len(received); i++ {
		laterStrictlyHigherPriority := prioritizer.Prior(received[i], received[i-1]) &&
			!prioritizer.Prior(received[i-1], received[i])
		require.Falsef(
			t,
			laterStrictlyHigherPriority,
			"later message outranked earlier one at index %d: %T -> %T",
			i-1, received[i-1].Body, received[i].Body,
		)
	}

	// --- 2) Assert we popped exactly the same counts per bucket ---
	actualCounts := make(map[int]int)
	for _, msg := range received {
		actualCounts[priority(msg)]++
	}

	require.Equal(t, expectedCounts, actualCounts)
}

// TestQueueLoadAndSaturationScenarios verifies committee queue handling under load and saturation conditions.
// It tests three key scenarios:
//
//   - drop when inbox strictly full: Confirms that when a queue's intake buffer reaches capacity,
//     HandleMessage drops incoming messages regardless of potential processability by ConsumeQueue.
//
//   - high priority messages dropped when queue full: Demonstrates that even highest-priority messages
//     like ExecuteDuty events are dropped when the queue's intake buffer is full.
//
//   - prioritization when saturated with filtered messages: Verifies that high-priority messages
//     (e.g., ExecuteDuty, Proposals) can still be processed even when many filtered messages
//     occupy the queue, provided the intake buffer has not reached absolute capacity.
//     This demonstrates the effectiveness of the prioritization mechanism.
//
// POSSIBLE ISSUE:
// This test suite has identified a potential issue with the queue's handling of messages under load and saturation conditions.
//
//  1. Message Dropping: When a queue fills up, new messages get dropped completely, even
//     high-priority ones. This happens because the queue uses a non-blocking approach when
//     receiving new messages. If there's no space, messages are discarded rather than waiting
//     for space to become available. This means important messages could be lost during periods
//     of heavy load or when messages get stuck in the queue due to filtering rules.
//
// This test suite highlights a known behavior where the queue drops messages when full,
// even high-priority ones, due to its non-blocking intake.
// This behavior and potential improvements are tracked
// in GitHub issue #1680 (https://github.com/ssvlabs/ssv/issues/1680).
func TestQueueLoadAndSaturationScenarios(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := log.TestLogger(t)

	t.Run("drop when inbox strictly full", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		logger = logger.Named("DropWhenInboxStrictlyFull")

		slot := phase0.Slot(123)

		committee := &Committee{
			logger:          logger,
			networkConfig:   networkconfig.TestNetwork,
			Queues:          make(map[phase0.Slot]queueContainer),
			Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
			CommitteeMember: &spectypes.CommitteeMember{},
		}

		currentRound := specqbft.Round(1)
		nextRound := specqbft.Round(2)
		queueCapacity := 3

		qContainer := queue.New(logger, queueCapacity)
		qState := newCommitteeQueueStateForTest(slot, currentRound, true, committee.CommitteeMember.GetQuorum())
		committee.Queues[slot] = queueContainer{Q: qContainer}

		// 1. Fill the queue's inbox channel to capacity using HandleMessage.
		for i := range queueCapacity {
			msgID := spectypes.MessageID{byte(i + 1)}
			prepareMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType}
			testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, prepareMsgBody)
			committee.EnqueueMessage(ctx, testMsg)
		}
		require.Equal(t, queueCapacity, qContainer.Len())

		// 2. Attempt to HandleMessage a new Prepare message for the *nextRound*.
		poppableMsgBody := &specqbft.Message{Height: specqbft.Height(slot), Round: nextRound, MsgType: specqbft.PrepareMsgType}
		poppableTestMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{0xAA}, poppableMsgBody)
		committee.EnqueueMessage(ctx, poppableTestMsg)

		// 3. Verify the poppable message was dropped.
		assert.Equal(t, queueCapacity, qContainer.Len())

		// 4. Verify the content of the queue.
		drainedMessages := make([]*queue.SSVMessage, 0, queueCapacity)
		for i := 0; i < queueCapacity; i++ {
			popCtx, popCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			msg := qContainer.Pop(popCtx, queue.NewCommitteeQueuePrioritizer(qState), queue.FilterAny)
			popCancel() // Ensure cancellation happens after Pop or timeout
			require.NotNil(t, msg)
			drainedMessages = append(drainedMessages, msg)
		}

		finalPopCtx, finalPopCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer finalPopCancel()

		assert.Nil(t, qContainer.Pop(finalPopCtx, queue.NewCommitteeQueuePrioritizer(qState), queue.FilterAny), "Queue should be empty after draining initial messages")

		foundNextRoundMessage := false
		for _, msg := range drainedMessages {
			if qbftMsg, ok := msg.Body.(*specqbft.Message); ok {
				if qbftMsg.Round == nextRound {
					foundNextRoundMessage = true
					break
				}
				assert.Equal(t, currentRound, qbftMsg.Round)
			}
		}
		assert.False(t, foundNextRoundMessage)
	})

	t.Run("high priority messages dropped when queue full", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		logger = logger.Named("HighPriorityDropping")

		slot := phase0.Slot(789)

		committee := &Committee{
			logger:          logger,
			networkConfig:   networkconfig.TestNetwork,
			Queues:          make(map[phase0.Slot]queueContainer),
			Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
			CommitteeMember: &spectypes.CommitteeMember{},
		}

		currentRound := specqbft.Round(1)
		queueCapacity := 3

		qContainer := queue.New(logger, queueCapacity)
		qState := newCommitteeQueueStateForTest(slot, currentRound, true, committee.CommitteeMember.GetQuorum())
		committee.Queues[slot] = queueContainer{Q: qContainer}

		// 1. Fill the queue with low-priority consensus messages
		for i := 0; i < queueCapacity; i++ {
			msgID := spectypes.MessageID{byte(i + 1)}
			commitMsgBody := &specqbft.Message{
				Height:  specqbft.Height(slot),
				Round:   currentRound,
				MsgType: specqbft.CommitMsgType, // Low priority consensus message
			}
			testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, commitMsgBody)
			committee.EnqueueMessage(ctx, testMsg)
		}
		require.Equal(t, queueCapacity, qContainer.Len(), "Queue should be at capacity")

		// 2. Try to add a high-priority proposal message (proposals are higher priority than commits)
		highPriorityMsgBody := &specqbft.Message{
			Height:  specqbft.Height(slot),
			Round:   currentRound,
			MsgType: specqbft.ProposalMsgType, // Higher priority than commit
		}
		highPriorityMsg := makeTestSSVMessage(
			t,
			spectypes.SSVConsensusMsgType,
			spectypes.MessageID{0xEE},
			highPriorityMsgBody,
		)

		// Attempt to push using HandleMessage
		// This should be dropped because the queue is full, even though it's a higher priority message
		committee.EnqueueMessage(ctx, highPriorityMsg)

		// 3. Verify queue length still at capacity
		assert.Equal(t, queueCapacity, qContainer.Len())

		// 4. Verify only the original messages are in the queue
		drainedMessages := make([]*queue.SSVMessage, 0, queueCapacity)
		for i := 0; i < queueCapacity; i++ {
			popCtx, popCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			msg := qContainer.Pop(popCtx, queue.NewCommitteeQueuePrioritizer(qState), queue.FilterAny)
			popCancel()
			require.NotNil(t, msg)
			drainedMessages = append(drainedMessages, msg)
		}

		// Verify no high priority proposal message in the queue
		foundHighPriorityMessage := false
		for _, msg := range drainedMessages {
			if msg.MsgType == spectypes.SSVConsensusMsgType {
				if qbftMsg, ok := msg.Body.(*specqbft.Message); ok && qbftMsg.MsgType == specqbft.ProposalMsgType {
					foundHighPriorityMessage = true
					break
				}
			}
		}
		assert.False(t, foundHighPriorityMessage)

		// Verify all drained messages are the original commits
		commitCount := 0
		for _, msg := range drainedMessages {
			if msg.MsgType == spectypes.SSVConsensusMsgType {
				if qbftMsg, ok := msg.Body.(*specqbft.Message); ok && qbftMsg.MsgType == specqbft.CommitMsgType {
					commitCount++
				}
			}
		}
		assert.Equal(t, queueCapacity, commitCount)
	})

	t.Run("prioritization when saturated with filtered messages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		logger := logger.Named("PrioritizationWhenSaturated")

		committee := &Committee{
			networkConfig:   networkconfig.TestNetwork,
			Queues:          make(map[phase0.Slot]queueContainer),
			Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
			CommitteeMember: &spectypes.CommitteeMember{},
		}

		queueCapacity := 5
		currentRound := specqbft.Round(1)
		slot := phase0.Slot(456)

		committeeRunner := newCommitteeRunnerForTest(slot, currentRound, false, nil)

		q := queue.New(logger, queueCapacity)

		var (
			processedMsgs    []*queue.SSVMessage
			processMsgsMutex sync.Mutex
			handlerCalled    = make(chan struct{}, queueCapacity*3)
		)

		processFn := func(_ context.Context, _ *zap.Logger, msg *queue.SSVMessage) error {
			processMsgsMutex.Lock()
			processedMsgs = append(processedMsgs, msg)
			processMsgsMutex.Unlock()
			handlerCalled <- struct{}{}
			return nil
		}

		consumerCtx, consumerCancel := context.WithCancel(ctx)
		var consumerWg sync.WaitGroup
		consumerWg.Add(1)

		go func() {
			defer consumerWg.Done()
			committee.ConsumeQueue(consumerCtx, logger, queueContainer{Q: q}, processFn, committeeRunner)
		}()

		// Fill with filtered Prepare messages
		for i := 0; i < queueCapacity; i++ {
			msgID := spectypes.MessageID{byte(i + 1)}
			prepare := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.PrepareMsgType}
			require.True(t, q.TryPush(makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, prepare)))
		}

		time.Sleep(400 * time.Millisecond) // Give time for the consumer to process (and filter) messages

		select {
		case <-handlerCalled:
			t.Fatal("filtered message was processed too early")
		default: // Expected
		}

		// Push ExecuteDuty
		execData, _ := json.Marshal(&types.ExecuteCommitteeDutyData{Duty: &spectypes.CommitteeDuty{Slot: slot}})
		execMsg := makeTestSSVMessage(t, message.SSVEventMsgType, spectypes.MessageID{0xEE}, &types.EventMsg{Type: types.ExecuteDuty, Data: execData})
		require.True(t, q.TryPush(execMsg))
		select {
		case <-handlerCalled: // Good
		case <-time.After(1 * time.Second):
			t.Fatal("timed out on ExecuteDuty")
		}

		// Push Proposal
		proposal := &specqbft.Message{Height: specqbft.Height(slot), Round: currentRound, MsgType: specqbft.ProposalMsgType}
		propMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, spectypes.MessageID{0xFF}, proposal)
		require.True(t, q.TryPush(propMsg))
		select {
		case <-handlerCalled: // Good
		case <-time.After(1 * time.Second):
			t.Fatal("timed out on Proposal")
		}

		// Now flip runner state so prepares become valid
		consumerCancel()  // Stop the current consumer
		consumerWg.Wait() // Wait for it to finish

		ctx2, cancel2 := context.WithCancel(ctx)
		defer cancel2()

		committeeRunner.State.RunningInstance.State.ProposalAcceptedForCurrentRound =
			&specqbft.ProcessingMessage{QBFTMessage: proposal}

		// Restart consumption
		consumer2Ctx, consumer2Cancel := context.WithCancel(ctx2)
		var consumer2Wg sync.WaitGroup
		consumer2Wg.Add(1)

		go func() {
			defer consumer2Wg.Done()
			committee.ConsumeQueue(consumer2Ctx, logger, queueContainer{Q: q}, processFn, committeeRunner)
		}()

		// Observe how many of the old Prepares now drain
		timeout := time.After(2 * time.Second)
		for done := false; !done; {
			select {
			case <-handlerCalled:
			case <-timeout:
				done = true
			case <-ctx2.Done():
				done = true
			}
		}

		consumer2Cancel()
		consumer2Wg.Wait()

		processMsgsMutex.Lock()
		var sawExec, sawProp bool
		for _, m := range processedMsgs {
			if m.MsgType == message.SSVEventMsgType {
				if e, ok := m.Body.(*types.EventMsg); ok && e.Type == types.ExecuteDuty {
					sawExec = true
				}
			}
			if m.MsgType == spectypes.SSVConsensusMsgType {
				if b, ok := m.Body.(*specqbft.Message); ok && b.MsgType == specqbft.ProposalMsgType {
					sawProp = true
				}
			}
		}
		processMsgsMutex.Unlock()

		assert.True(t, sawExec)
		assert.True(t, sawProp)
	})
}
