package connections

import (
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

func TestNetworkIDFilter(t *testing.T) {
	prepareTestingData()

	f := NetworkIDFilter("xxx")

	ok, err := f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "xxx",
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "bbb",
		},
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestSenderRecipientIPsCheckFilter(t *testing.T) {
	prepareTestingData()

	f := SenderRecipientIPsCheckFilter(RecipientPeerID)

	ok, err := f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    SenderPeerID,
			RecipientPeerID: RecipientPeerID,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    "wrong sender",
			RecipientPeerID: RecipientPeerID,
		},
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f(SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    SenderPeerID,
			RecipientPeerID: "wrong recipient",
		},
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestSignatureCheckFFilter(t *testing.T) {
	prepareTestingData()

	f := SignatureCheckFilter()

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: HandshakeData,
		Signature:     Signature,
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{}, //wrong handshake data
		Signature:     Signature,
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: HandshakeData,
		Signature:     []byte("wrong signature"),
	})
	require.Error(t, err)
	require.False(t, ok)

	wrongTimestamp := HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: wrongTimestamp,
		Signature:     Signature,
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestRegisteredOperatorsFilter(t *testing.T) {
	prepareTestingData()

	f := RegisteredOperatorsFilter(logging.TestLogger(t), mock.NodeStorage{
		RegisteredOperatorPublicKeyPEMs: [][]byte{
			SenderPublicKeyPEM,
		},
	})

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: HandshakeData,
	})
	require.NoError(t, err)
	require.True(t, ok)

	wrongSenderPubKeyPem := HandshakeData
	wrongSenderPubKeyPem.SenderPubKeyPem = []byte{'w', 'r', 'o', 'n', 'g'}
	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: wrongSenderPubKeyPem,
	})
	require.Error(t, err)
	require.False(t, ok)
}
