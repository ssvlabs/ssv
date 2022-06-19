package types

type Encoder interface {
	// Encode returns the encoded struct in bytes or error
	Encode() ([]byte, error)
	// Decode returns error if decoding failed
	Decode(data []byte) error
}
