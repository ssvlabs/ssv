package message

// Encoder encodes or decodes the message
type Encoder interface {
	// Encode encodes the message
	Encode() ([]byte, error)
	// Decode decodes the message
	Decode(data []byte) error
}
