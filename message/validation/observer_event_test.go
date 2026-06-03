package validation

import (
	"context"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pspb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type captureSSVValidationObserver struct {
	event SSVValidationEvent
}

func (o *captureSSVValidationObserver) ObserveSSVValidation(_ context.Context, _ *zap.Logger, event SSVValidationEvent) {
	o.event = event
}

func TestHandleValidationErrorObservesStageTopicAndPayload(t *testing.T) {
	observer := &captureSSVValidationObserver{}
	mv := &messageValidator{
		logger:   zap.NewNop(),
		observer: observer,
	}

	topic := "ssv.v2.42"
	pmsg := &pubsub.Message{
		Message: &pspb.Message{
			Topic: &topic,
			Data:  []byte{1, 2, 3, 4},
		},
	}
	pid := peer.ID("peer-a")

	result := mv.handleValidationError(t.Context(), pid, nil, pmsg, withValidationStage(SSVValidationStageDecodeSigned, ErrMalformedPubSubMessage))

	require.Equal(t, pubsub.ValidationReject, result)
	require.Equal(t, pid, observer.event.PeerID)
	require.Equal(t, SSVValidationRejected, observer.event.Outcome)
	require.Equal(t, ErrMalformedPubSubMessage.Text(), observer.event.Reason)
	require.Equal(t, SSVValidationStageDecodeSigned, observer.event.Stage)
	require.Equal(t, topic, observer.event.Topic)
	require.Equal(t, 4, observer.event.PayloadSize)
}

func TestWithValidationStagePreservesErrorMatching(t *testing.T) {
	err := withValidationStage(SSVValidationStagePubsubBasic, ErrPubSubMessageHasNoData)

	require.ErrorIs(t, err, ErrPubSubMessageHasNoData)
	require.Equal(t, SSVValidationStagePubsubBasic, validationStageFromError(err))
}
