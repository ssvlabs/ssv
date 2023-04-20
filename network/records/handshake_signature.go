package records

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type HandshakeSignature struct {
	SenderPeerID    peer.ID
	RecipientPeerID peer.ID
	Timestamp       time.Time // Unix timestamp ROUNDED TO 30 SECONDS
}
