package runner

import (
	"errors"
	"fmt"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRetryableError(t *testing.T) {
	someErr := fmt.Errorf("some error")
	require.False(t, IsRetryable(someErr))

	wrappedErr := NewRetryableError(someErr)
	require.True(t, IsRetryable(wrappedErr))
}

// TestIsRecoverableReconstructError pins the classifier the post-consensus defer relies on: only a
// tagged error is recoverable, and the tag survives further wrapping. Crucially, it also verifies the
// tag never hides the wrapped chain — a code-tagged *spectypes.Error stays reachable via errors.As, so
// the committee role still observably emits the reconstruct error code even after tagging.
func TestIsRecoverableReconstructError(t *testing.T) {
	t.Run("plain error is not recoverable", func(t *testing.T) {
		require.False(t, isRecoverableReconstructError(errors.New("boom")))
	})

	t.Run("nil is not recoverable", func(t *testing.T) {
		require.False(t, isRecoverableReconstructError(nil))
	})

	t.Run("tagged error is recoverable", func(t *testing.T) {
		tagged := recoverableReconstructError{errors.New("could not reconstruct")}
		require.True(t, isRecoverableReconstructError(tagged))
	})

	t.Run("tag survives further wrapping", func(t *testing.T) {
		tagged := recoverableReconstructError{errors.New("inner")}
		wrapped := fmt.Errorf("submit stage: %w", tagged)
		require.True(t, isRecoverableReconstructError(wrapped))
	})

	t.Run("wrapped coded spec error stays reachable via errors.As", func(t *testing.T) {
		coded := spectypes.NewError(spectypes.ReconstructSignatureErrorCode, "could not reconstruct a valid signature")
		tagged := recoverableReconstructError{fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", coded)}

		require.True(t, isRecoverableReconstructError(tagged))

		var specErr *spectypes.Error
		require.ErrorAs(t, tagged, &specErr, "the tag must not hide the coded spec error underneath")
		require.Equal(t, spectypes.ReconstructSignatureErrorCode, specErr.Code)
	})
}
