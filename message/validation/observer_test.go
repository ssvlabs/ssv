package validation

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type stubSSVValidationObserver struct {
	interested bool
	events     []SSVValidationEvent
}

func (s *stubSSVValidationObserver) Interested(peer.ID) bool {
	return s.interested
}

func (s *stubSSVValidationObserver) ObserveSSVValidation(_ context.Context, _ *zap.Logger, event SSVValidationEvent) {
	// The event's reference fields are shared with the validator and only valid
	// for the duration of this call, so clone them before retaining the event.
	event.DutyExecutorID = slices.Clone(event.DutyExecutorID)
	event.Signers = slices.Clone(event.Signers)
	if event.Consensus != nil {
		consensus := *event.Consensus
		event.Consensus = &consensus
	}
	s.events = append(s.events, event)
}

func TestHandleValidationErrorReportsOutcomeToObserver(t *testing.T) {
	observer := &stubSSVValidationObserver{interested: true}
	mv := &messageValidator{logger: zap.NewNop(), observer: observer}
	pid := peer.ID("highlighted")

	dutyExecutorID := bytes.Repeat([]byte{0xab}, 48)
	decoded := &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.DomainType{1, 2, 3, 4}, dutyExecutorID, spectypes.RoleProposer),
		},
		SignedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1, 2}},
		Body:             &specqbft.Message{MsgType: specqbft.PrepareMsgType, Height: 10, Round: 2},
	}

	require.Equal(t, pubsub.ValidationReject, mv.handleValidationError(t.Context(), pid, decoded, ErrZeroRound))
	require.Equal(t, pubsub.ValidationIgnore, mv.handleValidationError(t.Context(), pid, nil, Error{text: "stale message"}))
	require.Equal(t, pubsub.ValidationIgnore, mv.handleValidationError(t.Context(), pid, nil, errors.New("boom")))

	require.Len(t, observer.events, 3)

	rejected := observer.events[0]
	require.Equal(t, SSVValidationRejected, rejected.Outcome)
	require.Equal(t, ErrZeroRound.Text(), rejected.Reason)
	require.Equal(t, pid, rejected.PeerID)
	require.Equal(t, spectypes.RoleProposer, rejected.Role)
	require.Equal(t, spectypes.SSVConsensusMsgType, rejected.SSVMessageType)
	require.Equal(t, phase0.Slot(10), rejected.Slot)
	require.Equal(t, dutyExecutorID, rejected.DutyExecutorID)
	require.Equal(t, []spectypes.OperatorID{1, 2}, rejected.Signers)
	require.NotNil(t, rejected.Consensus)
	require.Equal(t, specqbft.Round(2), rejected.Consensus.Round)
	require.Equal(t, specqbft.PrepareMsgType, rejected.Consensus.QBFTMessageType)

	require.Equal(t, SSVValidationIgnored, observer.events[1].Outcome)
	require.Equal(t, "stale message", observer.events[1].Reason)

	require.Equal(t, SSVValidationIgnored, observer.events[2].Outcome)
	require.Equal(t, validationUnexpectedErrorReason, observer.events[2].Reason)
	require.Equal(t, "boom", observer.events[2].Error)
}

func TestHandleValidationSuccessReportsAcceptedToObserver(t *testing.T) {
	observer := &stubSSVValidationObserver{interested: true}
	mv := &messageValidator{logger: zap.NewNop(), observer: observer}
	pid := peer.ID("highlighted")

	require.Equal(t, pubsub.ValidationAccept, mv.handleValidationSuccess(t.Context(), pid, nil))

	require.Len(t, observer.events, 1)
	require.Equal(t, SSVValidationAccepted, observer.events[0].Outcome)
	require.Equal(t, "valid", observer.events[0].Reason)
	require.Equal(t, pid, observer.events[0].PeerID)
}

func TestSSVValidationObserverSkippedForUninterestingPeer(t *testing.T) {
	observer := &stubSSVValidationObserver{interested: false}
	mv := &messageValidator{logger: zap.NewNop(), observer: observer}
	pid := peer.ID("regular")

	require.Equal(t, pubsub.ValidationAccept, mv.handleValidationSuccess(t.Context(), pid, nil))
	require.Equal(t, pubsub.ValidationReject, mv.handleValidationError(t.Context(), pid, nil, ErrZeroRound))

	require.Empty(t, observer.events)
}

func TestSSVValidationWithoutObserverDoesNotPanic(t *testing.T) {
	mv := &messageValidator{logger: zap.NewNop()}

	require.Equal(t, pubsub.ValidationAccept, mv.handleValidationSuccess(t.Context(), "peer", nil))
	require.Equal(t, pubsub.ValidationReject, mv.handleValidationError(t.Context(), "peer", nil, ErrZeroRound))
}
