package connections

import (
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

func TestNetworkIDFilter(t *testing.T) {
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
	testingData := getTestingData(t)

	f := SenderRecipientIPsCheckFilter(testingData.RecipientPeerID)

	ok, err := f(testingData.SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    testingData.SenderPeerID,
			RecipientPeerID: testingData.RecipientPeerID,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f(testingData.SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    "wrong sender",
			RecipientPeerID: testingData.RecipientPeerID,
		},
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f(testingData.SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    testingData.SenderPeerID,
			RecipientPeerID: "wrong recipient",
		},
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestSignatureCheckFFilter(t *testing.T) {
	testingData := getTestingData(t)

	f := SignatureCheckFilter()

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: testingData.HandshakeData,
		Signature:     testingData.Signature,
	})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{}, //wrong handshake data
		Signature:     testingData.Signature,
	})
	require.Error(t, err)
	require.False(t, ok)

	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: testingData.HandshakeData,
		Signature:     []byte("wrong signature"),
	})
	require.Error(t, err)
	require.False(t, ok)

	wrongTimestamp := testingData.HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: wrongTimestamp,
		Signature:     testingData.Signature,
	})
	require.Error(t, err)
	require.False(t, ok)
}

func TestRegisteredOperatorsFilter(t *testing.T) {
	testingData := getTestingData(t)

	f := RegisteredOperatorsFilter(logging.TestLogger(t), mock.NodeStorage{
		RegisteredOperatorPublicKeyPEMs: [][]byte{
			testingData.SenderPublicKeyPEM,
		},
	})

	ok, err := f("", &records.SignedNodeInfo{
		HandshakeData: testingData.HandshakeData,
	})
	require.NoError(t, err)
	require.True(t, ok)

	wrongSenderPubKeyPem := testingData.HandshakeData
	wrongSenderPubKeyPem.SenderPubKeyPem = []byte{'w', 'r', 'o', 'n', 'g'}
	ok, err = f("", &records.SignedNodeInfo{
		HandshakeData: wrongSenderPubKeyPem,
	})
	require.Error(t, err)
	require.False(t, ok)
}
