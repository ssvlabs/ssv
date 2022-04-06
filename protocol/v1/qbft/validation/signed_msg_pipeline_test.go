package validation

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIfFirstTrueContinueToSecond(t *testing.T) {
	validPipline := WrapFunc("valid", func(signedMessage *message.SignedMessage) error {
		return nil
	})

	invalidPipeline := WrapFunc("invalid", func(signedMessage *message.SignedMessage) error {
		return errors.Errorf("error")
	})

	t.Run("a valid, b valid", func(t *testing.T) {
		require.NoError(t, CombineQuiet(validPipline, validPipline).Run(nil))
	})

	t.Run("a valid, b invalid", func(t *testing.T) {
		require.EqualError(t, CombineQuiet(validPipline, invalidPipeline).Run(nil), "error")
	})

	t.Run("a invalid, b valid", func(t *testing.T) {
		require.NoError(t, CombineQuiet(invalidPipeline, validPipline).Run(nil))
	})

	t.Run("a invalid, b invalid", func(t *testing.T) {
		require.NoError(t, CombineQuiet(invalidPipeline, invalidPipeline).Run(nil))
	})
}
