package runner

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryableError(t *testing.T) {
	someErr := fmt.Errorf("some error")
	require.False(t, errors.Is(someErr, &RetryableError{}))

	retryableErr := NewRetryableError(someErr)
	require.True(t, errors.Is(retryableErr, &RetryableError{}))
}
