package records

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestHandshakeData_Hash(t *testing.T) {
	const expectedHashBase64 = "nAkWkk1gxFU/DeMLs6YfsyEvx1czhlY35gbwUzzvrbk="

	expectedHash, err := base64.StdEncoding.DecodeString(expectedHashBase64)
	require.NoError(t, err)

	handshakeData := HandshakeData{
		SenderPeerID:    peer.ID("1.1.1.1"),
		RecipientPeerID: peer.ID("2.2.2.2"),
		Timestamp:       time.Unix(1684228246, 0),
		SenderPublicKey: []byte("some key"),
	}

	actualHash := handshakeData.Hash()

	require.True(t, bytes.Equal(expectedHash, actualHash[:]))
}
