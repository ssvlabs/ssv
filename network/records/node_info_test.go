package records

import (
	crand "crypto/rand"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNodeInfo_Seal_Consume(t *testing.T) {
	netKey, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	ni := &NodeInfo{
		ForkVersion: forksprotocol.GenesisForkVersion,
		NetworkID:   "testnet",
		Metadata: &NodeMetadata{
			NodeVersion:   "v0.1.12",
			ExecutionNode: "geth/x",
			ConsensusNode: "prysm/x",
			OperatorID:    "xxx",
		},
	}

	data, err := ni.Seal(netKey)
	require.NoError(t, err)
	parsedRec := &NodeInfo{}
	require.NoError(t, parsedRec.Consume(data))

	require.Equal(t, ni.ForkVersion, parsedRec.ForkVersion)
	require.Equal(t, ni.NetworkID, parsedRec.NetworkID)
	require.Equal(t, ni.Metadata.NodeVersion, parsedRec.Metadata.NodeVersion)
	require.Equal(t, ni.Metadata.ExecutionNode, parsedRec.Metadata.ExecutionNode)
	require.Equal(t, ni.Metadata.ConsensusNode, parsedRec.Metadata.ConsensusNode)
	require.Equal(t, ni.Metadata.OperatorID, parsedRec.Metadata.OperatorID)
}
