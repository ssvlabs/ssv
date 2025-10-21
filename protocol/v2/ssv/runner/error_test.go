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

	wrappedErr := NewRetryableError(someErr)
	require.True(t, errors.Is(wrappedErr, &RetryableError{}))

	rErr := &RetryableError{}
	require.False(t, errors.As(someErr, &rErr))
	require.True(t, errors.As(wrappedErr, &rErr))
}
