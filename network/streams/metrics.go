package streams

import (
	"github.com/libp2p/go-libp2p/core/protocol"
)

type Metrics interface {
	OutgoingStreamRequest(protocol protocol.ID)
	AddActiveStreamRequest(protocol protocol.ID)
	RemoveActiveStreamRequest(protocol protocol.ID)
	SuccessfulStreamRequest(protocol protocol.ID)
	StreamResponse(protocol protocol.ID)
	StreamRequest(protocol protocol.ID)
}
