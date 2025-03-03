package ssvsignerclient

import (
	"testing"

	"github.com/pkg/errors"
)

func Test_ShareDecryptionError(t *testing.T) {
	var customErr error = ShareDecryptionError(errors.New("test error"))

	var shareDecryptionError ShareDecryptionError
	if !errors.As(customErr, &shareDecryptionError) {
		t.Errorf("shareDecryptionError was expected to be a ShareDecryptionError")
	}
}
