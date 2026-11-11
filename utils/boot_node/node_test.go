package bootnode

import (
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBootNode_ExternalIP checks that the ENR carries ExternalIP when it's set and the local IP otherwise, and that
// an ExternalIP that isn't an IP address is an error.
func TestBootNode_ExternalIP(t *testing.T) {
	privKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	localIP := net.IPv4(10, 0, 0, 5)

	tests := []struct {
		name       string
		externalIP string
		wantIP     string
		wantErr    string
	}{
		{name: "unset", wantIP: "10.0.0.5"},
		{name: "set", externalIP: "203.0.113.7", wantIP: "203.0.113.7"},
		{name: "invalid", externalIP: "203.0.113", wantErr: `invalid external IP "203.0.113"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &bootNode{logger: zap.NewNop(), externalIP: tt.externalIP, dbPath: t.TempDir(), tcpPort: 5000}
			localNode, err := n.createLocalNode(privKey, localIP, 4000)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			t.Cleanup(localNode.Database().Close)
			require.Equal(t, tt.wantIP, localNode.Node().IP().String())
		})
	}
}
