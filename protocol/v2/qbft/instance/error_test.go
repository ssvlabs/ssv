package instance

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryableError(t *testing.T) {
	someErr := fmt.Errorf("some error")
	require.False(t, IsRetryable(someErr))

	wrappedErr := NewRetryableError(someErr)
	require.True(t, IsRetryable(wrappedErr))
}
