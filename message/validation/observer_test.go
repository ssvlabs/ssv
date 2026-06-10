package validation

import (
	"context"
	"errors"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubSSVValidationObserver struct {
	interested bool
	events     []SSVValidationEvent
}

func (s *stubSSVValidationObserver) Interested(peer.ID) bool {
	return s.interested
}

func (s *stubSSVValidationObserver) ObserveSSVValidation(_ context.Context, _ *zap.Logger, event SSVValidationEvent) {
	s.events = append(s.events, event)
}

func TestHandleValidationErrorReportsOutcomeToObserver(t *testing.T) {
	observer := &stubSSVValidationObserver{interested: true}
	mv := &messageValidator{logger: zap.NewNop(), observer: observer}
	pid := peer.ID("highlighted")

	require.Equal(t, pubsub.ValidationReject, mv.handleValidationError(t.Context(), pid, nil, ErrZeroRound))
	require.Equal(t, pubsub.ValidationIgnore, mv.handleValidationError(t.Context(), pid, nil, Error{text: "stale message"}))
	require.Equal(t, pubsub.ValidationIgnore, mv.handleValidationError(t.Context(), pid, nil, errors.New("boom")))

	require.Len(t, observer.events, 3)

	require.Equal(t, SSVValidationRejected, observer.events[0].Outcome)
	require.Equal(t, ErrZeroRound.Text(), observer.events[0].Reason)
	require.Equal(t, pid, observer.events[0].PeerID)

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
