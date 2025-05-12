package validator

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	qbfttests "github.com/ssvlabs/ssv/integration/qbft/tests"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

// makeTestSSVMessage creates a test SignedSSVMessage for testing queue handling.
func makeTestSSVMessage(t *testing.T, msgType spectypes.MsgType, msgID spectypes.MessageID, body interface{}) *queue.SSVMessage {
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

// TestHandleMessageCreatesQueue verifies that HandleMessage creates a queue when none exists.
func TestHandleMessageCreatesQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)

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

// TestConsumeQueueBasic tests basic queue consumption functionality.
func TestConsumeQueueBasic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := zaptest.NewLogger(t)

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

	go func() {
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		assert.NoError(t, err)
	}()

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

// TestStartConsumeQueue verifies the StartConsumeQueue method handles various error conditions.
func TestStartConsumeQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)

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

// TestFilterNoProposalAccepted verifies filtering when no proposal is accepted,
// ensuring prepare and commit messages for the current round are skipped.
func TestFilterNoProposalAccepted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := zaptest.NewLogger(t)

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
						ProposalAcceptedForCurrentRound: nil, // No proposal accepted
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

	go func() {
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		assert.NoError(t, err)
	}()

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

// TestFilterNotDecidedSkipsPartialSignatures verifies that partial signature messages
// are filtered when the consensus instance is not yet decided.
func TestFilterNotDecidedSkipsPartialSignatures(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := zaptest.NewLogger(t)

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
						Decided:                         false, // Not decided yet
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

	go func() {
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		assert.NoError(t, err)
	}()

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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := zaptest.NewLogger(t)

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

	go func() {
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		assert.NoError(t, err)
	}()

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

// TestQueueCapacityOverflow verifies that messages are properly dropped
// when the queue reaches its capacity.
func TestQueueCapacityOverflow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	logger.Debug("starting queue capacity overflow test")

	slot := phase0.Slot(123)
	committee.Queues[slot] = queueContainer{
		Q: queue.New(3),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	smallQueueCapacity := 3

	q := queueContainer{
		Q: queue.New(smallQueueCapacity),
		queueState: &queue.State{
			HasRunningInstance: true,
			Height:             specqbft.Height(slot),
			Slot:               slot,
			Round:              1,
		},
	}

	for i := 0; i < smallQueueCapacity+2; i++ {
		msgID := spectypes.MessageID{byte(i), byte(i + 1), byte(i + 2), byte(i + 3)}
		qbftMsg := &specqbft.Message{
			Height:  specqbft.Height(slot),
			Round:   1,
			MsgType: specqbft.ProposalMsgType,
		}
		testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, qbftMsg)
		pushed := q.Q.TryPush(testMsg)

		if i < smallQueueCapacity {
			assert.True(t, pushed)
		} else {
			assert.False(t, pushed)
		}
	}

	assert.Equal(t, smallQueueCapacity, q.Q.Len())
}

// TestChangingFilterState verifies that messages that were previously filtered
// become processable when the filter state changes.
func TestChangingFilterState(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logger := zaptest.NewLogger(t)

	committee := &Committee{
		ctx:           ctx,
		Queues:        make(map[phase0.Slot]queueContainer),
		Runners:       make(map[phase0.Slot]*runner.CommitteeRunner),
		BeaconNetwork: qbfttests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
	}

	slot := phase0.Slot(123)
	currentRound := specqbft.Round(1)

	msgID := spectypes.MessageID{1, 2, 3, 4}
	prepareMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   currentRound,
		MsgType: specqbft.PrepareMsgType,
	}
	testMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, msgID, prepareMsg)

	proposalMsgID := spectypes.MessageID{5, 6, 7, 8}
	qbftProposalMsg := &specqbft.Message{
		Height:  specqbft.Height(slot),
		Round:   currentRound,
		MsgType: specqbft.ProposalMsgType,
	}
	proposalTestMsg := makeTestSSVMessage(t, spectypes.SSVConsensusMsgType, proposalMsgID, qbftProposalMsg)

	proposalMsg := &specqbft.ProcessingMessage{
		QBFTMessage: qbftProposalMsg,
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

	var mu sync.Mutex
	receivedMessages := make([]*queue.SSVMessage, 0)
	handlerCalled := make(chan struct{}, 2)

	handler := func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
		mu.Lock()
		receivedMessages = append(receivedMessages, msg)
		mu.Unlock()
		handlerCalled <- struct{}{}
		return nil
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

	go func() {
		err := committee.ConsumeQueue(ctx, q, logger, handler, committeeRunner)
		assert.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	q.Q.TryPush(testMsg)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	msgCount := len(receivedMessages)
	mu.Unlock()
	assert.Equal(t, 0, msgCount)

	committeeRunner.BaseRunner.State.RunningInstance.State.ProposalAcceptedForCurrentRound = proposalMsg

	q.Q.TryPush(proposalTestMsg)

	select {
	case <-handlerCalled:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for proposal message")
	}

	q.Q.TryPush(testMsg)

	select {
	case <-handlerCalled:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for prepare message")
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	receivedCount := len(receivedMessages)
	mu.Unlock()

	require.Equal(t, 2, receivedCount)

	if receivedCount >= 2 {
		assert.Equal(t, specqbft.ProposalMsgType, receivedMessages[0].Body.(*specqbft.Message).MsgType)
		assert.Equal(t, specqbft.PrepareMsgType, receivedMessages[1].Body.(*specqbft.Message).MsgType)
	}
}
