package records

import (
	"crypto/rand"
	"reflect"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/operator/keys"
)

func TestSignedNodeInfo_Seal_Consume(t *testing.T) {
	nodeInfo := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			Subnets:       "some-subnets",
		},
	}

	senderPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	handshakeData := HandshakeData{
		SenderPeerID:    peer.ID("1.1.1.1"),
		RecipientPeerID: peer.ID("2.2.2.2"),
		Timestamp:       time.Now().Round(time.Second),
		SenderPublicKey: senderPrivateKey.Base64(),
	}

	signature, err := senderPrivateKey.Sign(handshakeData.Encode())
	require.NoError(t, err)

	sni := &SignedNodeInfo{
		NodeInfo:      nodeInfo,
		HandshakeData: handshakeData,
		Signature:     signature,
	}

	netKey, _, err := libp2pcrypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)

	data, err := sni.Seal(netKey)
	require.NoError(t, err)

	parsedRec := &SignedNodeInfo{}
	require.NoError(t, parsedRec.Consume(data))

	require.True(t, reflect.DeepEqual(sni, parsedRec))
}

func TestSignedNodeInfo_Marshal_Unmarshal(t *testing.T) {
	nodeInfo := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			Subnets:       "some-subnets",
		},
	}

	senderPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	handshakeData := HandshakeData{
		SenderPeerID:    peer.ID("1.1.1.1"),
		RecipientPeerID: peer.ID("2.2.2.2"),
		Timestamp:       time.Now().Round(time.Second),
		SenderPublicKey: senderPrivateKey.Base64(),
	}

	signature, err := senderPrivateKey.Sign(handshakeData.Encode())
	require.NoError(t, err)

	sni := &SignedNodeInfo{
		NodeInfo:      nodeInfo,
		HandshakeData: handshakeData,
		Signature:     signature,
	}

	data, err := sni.MarshalRecord()
	require.NoError(t, err)

	parsedRec := &SignedNodeInfo{}
	require.NoError(t, parsedRec.UnmarshalRecord(data))

	require.True(t, reflect.DeepEqual(sni, parsedRec))
}
