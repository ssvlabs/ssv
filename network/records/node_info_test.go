package records

import (
	crand "crypto/rand"
	"reflect"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

func TestNodeInfo_Seal_Consume(t *testing.T) {
	netKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	ni := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			Subnets:       commons.AllSubnets.StringHex(),
		},
	}

	data, err := ni.Seal(netKey)
	require.NoError(t, err)

	parsedRec := &NodeInfo{}
	require.NoError(t, parsedRec.Consume(data))

	require.True(t, reflect.DeepEqual(ni, parsedRec))
}

// TestNodeInfo_Clone_NilSafety covers the three nil shapes Clone can encounter:
// a nil NodeInfo receiver, a NodeInfo with nil Metadata, and a nil NodeMetadata
// receiver. These matter because peersIndex.Self/UpdateSelfRecord clone on
// every call, and a missing Metadata block (e.g. a peer that handshakes with a
// 2-entry NodeInfo envelope) would otherwise nil-deref inside Clone before any
// caller-side guard runs.
func TestNodeInfo_Clone_NilSafety(t *testing.T) {
	t.Run("nil NodeInfo receiver", func(t *testing.T) {
		var ni *NodeInfo
		require.Nil(t, ni.Clone())
	})

	t.Run("nil Metadata field", func(t *testing.T) {
		ni := &NodeInfo{NetworkID: "testnet"}
		cloned := ni.Clone()
		require.NotNil(t, cloned)
		require.Equal(t, "testnet", cloned.NetworkID)
		require.Nil(t, cloned.Metadata)
	})

	t.Run("nil NodeMetadata receiver", func(t *testing.T) {
		var nm *NodeMetadata
		require.Nil(t, nm.Clone())
	})
}

func TestNodeInfo_Marshal_Unmarshal(t *testing.T) {
	oldSerializedData := []byte(`{"Entries":["", "testnet", "{\"NodeVersion\":\"v0.1.12\",\"ExecutionNode\":\"geth/x\",\"ConsensusNode\":\"prysm/x\",\"Subnets\":\"ffffffffffffffffffffffffffffffff\"}"]}`)

	currentSerializedData := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			Subnets:       commons.AllSubnets.StringHex(),
		},
	}

	data, err := currentSerializedData.MarshalRecord()
	require.NoError(t, err)

	parsedRec := &NodeInfo{}
	require.NoError(t, parsedRec.UnmarshalRecord(data))

	// Attempt to unmarshal old data into the latest version of NodeInfo
	require.NoError(t, parsedRec.UnmarshalRecord(oldSerializedData))

	require.True(t, reflect.DeepEqual(currentSerializedData, parsedRec))
}
