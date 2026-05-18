package topics

import (
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
)

func TestValidationResultString(t *testing.T) {
	require.Equal(t, "accept", validationResultString(pubsub.ValidationAccept))
	require.Equal(t, "reject", validationResultString(pubsub.ValidationReject))
	require.Equal(t, "ignore", validationResultString(pubsub.ValidationIgnore))
	require.Equal(t, "unknown", validationResultString(pubsub.ValidationResult(99)))
}
