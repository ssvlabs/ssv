package connections

import (
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers/connections/mock"
	"github.com/bloxapp/ssv/network/records"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"
)

func TestNetworkIDFilter(t *testing.T) {
	td := getTestingData(t)

	f := NetworkIDFilter("xxx")

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "xxx",
		},
	}))
	require.NoError(t, err)

	err = f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{
		NodeInfo: &records.NodeInfo{
			NetworkID: "bbb",
		},
	}))
	require.Error(t, err)
}

func TestSenderRecipientIPsCheckFilter(t *testing.T) {
	td := getTestingData(t)

	f := SenderRecipientIPsCheckFilter(td.RecipientPeerID)

	err := f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{
		HandshakeData: records.HandshakeData{
			SenderPeerID:    td.SenderPeerID,
			RecipientPeerID: td.RecipientPeerID,
		},
	}))
	require.NoError(t, err)

	err = f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{HandshakeData: records.HandshakeData{
		SenderPeerID:    "wrong sender",
		RecipientPeerID: td.RecipientPeerID,
	}}))
	require.Error(t, err)

	err = f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{HandshakeData: records.HandshakeData{
		SenderPeerID:    td.SenderPeerID,
		RecipientPeerID: "wrong recipient",
	}}))
	require.Error(t, err)
}

func TestSignatureCheckFFilter(t *testing.T) {
	td := getTestingData(t)

	f := SignatureCheckFilter()

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: td.HandshakeData, Signature: td.Signature}))
	require.NoError(t, err)

	//wrong handshake data
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: records.HandshakeData{}, Signature: td.Signature}))
	require.Error(t, err)

	err = f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: records.HandshakeData{}, Signature: []byte("wrong signature")}))
	require.Error(t, err)

	wrongTimestamp := td.HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: wrongTimestamp, Signature: td.Signature}))
	require.Error(t, err)
}

func TestRegisteredOperatorsFilter(t *testing.T) {
	td := getTestingData(t)

	f := RegisteredOperatorsFilter(logging.TestLogger(t), mock.NodeStorage{
		RegisteredOperatorPublicKeyPEMs: [][]byte{
			td.SenderPublicKeyPEM,
		},
	})

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: td.HandshakeData, Signature: td.Signature}))
	require.NoError(t, err)

	wrongSenderPubKeyPem := td.HandshakeData
	wrongSenderPubKeyPem.SenderPubKeyPem = []byte{'w', 'r', 'o', 'n', 'g'}
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, &records.SignedNodeInfo{NodeInfo: &records.NodeInfo{}, HandshakeData: wrongSenderPubKeyPem, Signature: td.Signature}))
	require.Error(t, err)
}

func SealAndConsume(t *testing.T, networkPrivateKey libp2pcrypto.PrivKey, sni *records.SignedNodeInfo) records.SignedNodeInfo {
	td := getTestingData(t)

	if sni.NodeInfo == nil {
		sni.NodeInfo = td.NodeInfo
	}

	sealed, err := sni.Seal(networkPrivateKey)
	require.NoError(t, err)

	sni = &records.SignedNodeInfo{}
	err = sni.Consume(sealed)
	require.NoError(t, err)

	return *sni
}
