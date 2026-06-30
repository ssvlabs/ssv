package goclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBeaconAddr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"141.95.98.197:32555", "http://141.95.98.197:32555"},                                                        // scheme-less host:port (config.yaml default)
		{"http://example.url:5052", "http://example.url:5052"},                                                       // already http
		{"https://beacon.glamsterdam-devnet-6.ethpandaops.io", "https://beacon.glamsterdam-devnet-6.ethpandaops.io"}, // already https
		{"user:pass@host:5052", "http://user:pass@host:5052"},                                                        // basic-auth preserved
		{"ethereum-beacon.blockpi.network/rpc/v1/KEY", "http://ethereum-beacon.blockpi.network/rpc/v1/KEY"},          // path prefix preserved
		{"http://host:5052/", "http://host:5052"},                                                                    // trailing slash trimmed
	} {
		require.Equal(t, tc.want, normalizeBeaconAddr(tc.in), "input %q", tc.in)
	}
}
