// Package wire provides shared low-level wire-envelope framing primitives
// used by SSV-side message-kind discriminators (notably tbft/wire and
// dkg/wire). Domain packages keep their own kind enums and dispatch logic;
// this package handles only the version-prefix-plus-kind framing.
//
// On-wire frame:
//
//	[1] envelope version (currently 0x01)
//	[1] kind discriminator (domain-specific)
//	[N] body bytes
package wire

import (
	"errors"
	"fmt"
)

// EnvelopeVersionV1 is the current frame version. Encoders write this;
// Unframe accepts only this version.
const EnvelopeVersionV1 byte = 0x01

// Frame prepends the version byte and `kind` discriminator to `body` and
// returns the bytes ready for the wire.
func Frame(kind byte, body []byte) []byte {
	out := make([]byte, 0, 2+len(body))
	out = append(out, EnvelopeVersionV1)
	out = append(out, kind)
	out = append(out, body...)
	return out
}

// Unframe parses a framed envelope. Returns the kind discriminator and the
// inner body bytes. Errors on truncation (< 2 bytes) and on unknown
// version. Domain packages dispatch on `kind` to decode the body further.
func Unframe(data []byte) (kind byte, body []byte, err error) {
	if len(data) < 2 {
		return 0, nil, errors.New("wire: envelope truncated (need at least version + kind)")
	}
	if data[0] != EnvelopeVersionV1 {
		return 0, nil, fmt.Errorf("wire: unsupported envelope version 0x%02x", data[0])
	}
	return data[1], data[2:], nil
}
