package api

import "encoding/hex"

type Hex []byte

func (h Hex) MarshalJSON() ([]byte, error) {
	return []byte("\"" + hex.EncodeToString(h) + "\""), nil
}
