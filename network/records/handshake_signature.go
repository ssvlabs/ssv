package records

import (
	"crypto/sha256"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type HandshakeData struct {
	SenderPeerID    peer.ID
	RecipientPeerID peer.ID
	Timestamp       time.Time // Unix timestamp ROUNDED TO 30 SECONDS
	SenderPubKey    string
}

func (h *HandshakeData) Hash() [32]byte {
	sb := strings.Builder{}

	sb.WriteString(h.SenderPeerID.String())
	sb.WriteString(h.RecipientPeerID.String())
	sb.WriteString(h.Timestamp.String())
	sb.WriteString(h.SenderPubKey)

	return sha256.Sum256([]byte(sb.String()))
}
