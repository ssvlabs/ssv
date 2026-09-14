package gloas

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// decodeHexInto decodes a 0x-prefixed hex string into dst, requiring exactly len(dst) bytes.
// field names the value in error messages.
func decodeHexInto(dst []byte, s, field string) error {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", field, err)
	}
	if len(b) != len(dst) {
		return fmt.Errorf("incorrect length for %s", field)
	}
	copy(dst, b)
	return nil
}
