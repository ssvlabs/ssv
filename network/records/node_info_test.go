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
			Subnets:       commons.AllSubnets,
		},
	}

	data, err := ni.Seal(netKey)
	require.NoError(t, err)

	parsedRec := &NodeInfo{}
	require.NoError(t, parsedRec.Consume(data))

	require.True(t, reflect.DeepEqual(ni, parsedRec))
}

func TestNodeInfo_Marshal_Unmarshal(t *testing.T) {
	oldSerializedData := []byte(`{"Entries":["", "testnet", "{\"NodeVersion\":\"v0.1.12\",\"ExecutionNode\":\"geth/x\",\"ConsensusNode\":\"prysm/x\",\"Subnets\":\"ffffffffffffffffffffffffffffffff\"}"]}`)

	currentSerializedData := &NodeInfo{
		NetworkID: "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			Subnets:       commons.AllSubnets,
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
