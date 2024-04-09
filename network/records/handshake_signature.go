package records

import (
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type HandshakeData struct {
	SenderPeerID    peer.ID
	RecipientPeerID peer.ID
	Timestamp       time.Time
	SenderPublicKey []byte
}

func (h *HandshakeData) Encode() []byte {
	sb := strings.Builder{}

	sb.WriteString(h.SenderPeerID.String())
	sb.WriteString(h.RecipientPeerID.String())
	sb.WriteString(strconv.FormatInt(h.Timestamp.Unix(), 10))
	sb.Write(h.SenderPublicKey)

	return []byte(sb.String())
}
