package eventhandler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner"
)

func TestShareDecryptionError(t *testing.T) {
	tt := []struct {
		name           string
		f              func() error
		malformedEvent bool
	}{
		{
			// LocalKeyManager.AddShare returns the error directly.
			name: "local decryption error is malformed",
			f: func() error {
				err1 := fmt.Errorf("some error")
				return ssvsigner.ShareDecryptionError{Err: fmt.Errorf("decrypt: %w", err1)}
			},
			malformedEvent: true,
		},
		{
			// RemoteKeyManager.AddShare wraps the client's ShareDecryptionError with
			// fmt.Errorf("add validator: %w", ...); errors.As must still classify the
			// wrapped error as a decryption error.
			name: "remote decryption error is malformed",
			f: func() error {
				clientErr := ssvsigner.ShareDecryptionError{Err: errors.New("decrypt: crypto/rsa: decryption error")}
				return fmt.Errorf("add validator: %w", clientErr)
			},
			malformedEvent: true,
		},
		{
			// A transport/other failure (not a share problem) must stay fatal so the node
			// doesn't silently skip an event it failed to process.
			name: "non-decryption error is fatal",
			f: func() error {
				e2 := fmt.Errorf("some error")
				e1 := fmt.Errorf("request failed: %w", e2)
				return fmt.Errorf("add validator: %w", e1)
			},
			malformedEvent: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			// The implementation might be more optimal,
			// but it's left purposely like this because it's close to the actual code.

			var resultErr error
			var malformedEvent bool

			if err := tc.f(); err != nil {
				var shareDecryptionError ssvsigner.ShareDecryptionError
				if errors.As(err, &shareDecryptionError) {
					resultErr = &MalformedEventError{Err: err}
					malformedEvent = true
				} else {
					resultErr = fmt.Errorf("could not add share encrypted key: %w", err)
					malformedEvent = false
				}
			}

			require.Error(t, resultErr)
			require.Equal(t, tc.malformedEvent, malformedEvent)
		})
	}
}
