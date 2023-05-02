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

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, td.HandshakeData, td.Signature, &records.NodeInfo{
		NetworkID: "xxx",
	}))
	require.NoError(t, err)

	err = f("", SealAndConsume(t, td.NetworkPrivateKey, td.HandshakeData, td.Signature, &records.NodeInfo{
		NetworkID: "bbb",
	}))
	require.Error(t, errPeerWasFiltered)
}

func TestSenderRecipientIPsCheckFilter(t *testing.T) {
	td := getTestingData(t)

	f := SenderRecipientIPsCheckFilter(td.RecipientPeerID)

	err := f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, records.HandshakeData{
		SenderPeerID:    td.SenderPeerID,
		RecipientPeerID: td.RecipientPeerID,
	}, td.Signature, &records.NodeInfo{}))
	require.NoError(t, err)

	err = f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, records.HandshakeData{
		SenderPeerID:    "wrong sender",
		RecipientPeerID: td.RecipientPeerID,
	}, td.Signature, &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)

	err = f(td.SenderPeerID, SealAndConsume(t, td.NetworkPrivateKey, records.HandshakeData{
		SenderPeerID:    td.SenderPeerID,
		RecipientPeerID: "wrong recipient",
	}, td.Signature, &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)
}

func TestSignatureCheckFFilter(t *testing.T) {
	td := getTestingData(t)

	f := SignatureCheckFilter()

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, td.HandshakeData, td.Signature, &records.NodeInfo{}))
	require.NoError(t, err)

	//wrong handshake data
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, records.HandshakeData{}, td.Signature, &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)

	err = f("", SealAndConsume(t, td.NetworkPrivateKey, td.HandshakeData, []byte("wrong signature"), &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)

	wrongTimestamp := td.HandshakeData
	wrongTimestamp.Timestamp = wrongTimestamp.Timestamp.Add(-2 * AllowedDifference)
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, wrongTimestamp, td.Signature, &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)
}

func TestRegisteredOperatorsFilter(t *testing.T) {
	td := getTestingData(t)

	f := RegisteredOperatorsFilter(logging.TestLogger(t), mock.NodeStorage{
		RegisteredOperatorPublicKeyPEMs: [][]byte{
			td.SenderPublicKeyPEM,
		},
	})

	err := f("", SealAndConsume(t, td.NetworkPrivateKey, td.HandshakeData, td.Signature, &records.NodeInfo{}))
	require.NoError(t, err)

	wrongSenderPubKeyPem := td.HandshakeData
	wrongSenderPubKeyPem.SenderPubKeyPem = []byte{'w', 'r', 'o', 'n', 'g'}
	err = f("", SealAndConsume(t, td.NetworkPrivateKey, wrongSenderPubKeyPem, td.Signature, &records.NodeInfo{}))
	require.Error(t, errPeerWasFiltered)
}

func SealAndConsume(t *testing.T, networkPrivateKey libp2pcrypto.PrivKey, handshakeData records.HandshakeData, signature []byte, ni *records.NodeInfo) *records.SignedNodeInfo {
	sealed, err := ni.Seal(networkPrivateKey, handshakeData, signature)
	require.NoError(t, err)

	sni := &records.SignedNodeInfo{}
	err = sni.Consume(sealed)
	require.NoError(t, err)

	return sni
}
