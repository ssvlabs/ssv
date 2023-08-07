package connections

import (
	"testing"

	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	"github.com/stretchr/testify/require"
)

func TestNetworkIDFilter(t *testing.T) {
	f := NetworkIDFilter("xxx")

	err := f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "xxx",
		},
	})
	require.NoError(t, err)

	err = f("", &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "bbb",
		},
	})
	require.Error(t, err)
}

func TestSenderRecipientIPsCheckFilter(t *testing.T) {
	td := getTestingData(t)

	f := SenderRecipientIPsCheckFilter(td.RecipientPeerID)

	err := f(td.SenderPeerID, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    td.SenderPeerID,
			RecipientPeerID: td.RecipientPeerID,
		},
	})
	require.NoError(t, err)

	err = f(td.SenderPeerID, &records.SignedNodeInfo{HandshakeData: records.HandshakeData{
		SenderPeerID:    "wrong sender",
		RecipientPeerID: td.RecipientPeerID,
	}})
	require.Error(t, err)

	err = f(td.SenderPeerID, &records.SignedNodeInfo{HandshakeData: records.HandshakeData{
		SenderPeerID:    td.SenderPeerID,
		RecipientPeerID: "wrong recipient",
	}})
	require.Error(t, err)
}

func TestSignatureCheckFFilter(t *testing.T) {
	td := getTestingData(t)

	f := SignatureCheckFilter()

	err := f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: td.HandshakeData, Signature: td.Signature})
	require.NoError(t, err)

	//wrong handshake data
	err = f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: records.HandshakeData{}, Signature: td.Signature})
	require.Error(t, err)

	err = f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: records.HandshakeData{}, Signature: []byte("wrong signature")})
	require.Error(t, err)

	wrongTimestamp := td.HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	err = f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: wrongTimestamp, Signature: td.Signature})
	require.Error(t, err)
}

func TestRegisteredOperatorsFilter_ContractRegistered(t *testing.T) {
	td := getTestingData(t)

	f := RegisteredOperatorsFilter(mock.NodeStorage{
		RegisteredOperatorPublicKeyPEMs: []string{
			td.SenderBase64PublicKeyPEM,
		},
	}, nil)

	err := f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: td.HandshakeData, Signature: td.Signature})
	require.NoError(t, err)

	wrongSenderPubKeyPem := td.HandshakeData
	wrongSenderPubKeyPem.SenderPublicKey = []byte{'w', 'r', 'o', 'n', 'g'}
	err = f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: wrongSenderPubKeyPem, Signature: td.Signature})
	require.Error(t, err)
}

func TestRegisteredOperatorsFilter_ConfigWhitelist(t *testing.T) {
	td := getTestingData(t)

	f := RegisteredOperatorsFilter(mock.NodeStorage{}, []string{td.SenderBase64PublicKeyPEM})

	err := f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: td.HandshakeData, Signature: td.Signature})
	require.NoError(t, err)

	wrongSenderPubKeyPem := td.HandshakeData
	wrongSenderPubKeyPem.SenderPublicKey = []byte{'w', 'r', 'o', 'n', 'g'}
	err = f("", &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: wrongSenderPubKeyPem, Signature: td.Signature})
	require.Error(t, err)
}
