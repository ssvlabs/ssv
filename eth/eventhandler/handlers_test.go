package eventhandler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

func TestShareDecryptionError(t *testing.T) {
	tt := []struct {
		name           string
		f              func() error
		malformedEvent bool
	}{
		{
			name: "malformed event",
			f: func() error {
				err1 := fmt.Errorf("some error")
				return ekm.ShareDecryptionError{Err: fmt.Errorf("decrypt: %w", err1)}
			},
			malformedEvent: true,
		},

		{
			name: "no malformed event event",
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
				var shareDecryptionEKMError ekm.ShareDecryptionError
				if errors.As(err, &shareDecryptionEKMError) {
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
