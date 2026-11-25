package goclient

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNormalizeBeaconAddr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"141.95.98.197:32555", "http://141.95.98.197:32555"},                                                        // scheme-less host:port (config.yaml default)
		{"http://example.url:5052", "http://example.url:5052"},                                                       // already http
		{"https://beacon.glamsterdam-devnet-6.ethpandaops.io", "https://beacon.glamsterdam-devnet-6.ethpandaops.io"}, // already https
		{"user:pass@host:5052", "http://user:pass@host:5052"},                                                        // basic-auth preserved
		{"ethereum-beacon.blockpi.network/rpc/v1/KEY", "http://ethereum-beacon.blockpi.network/rpc/v1/KEY"},          // path prefix preserved
		{"http://host:5052/", "http://host:5052"},                                                                    // trailing slash trimmed
		{"http.example.com:5052", "http://http.example.com:5052"},                                                    // scheme-less host that starts with "http"
	} {
		require.Equal(t, tc.want, normalizeBeaconAddr(tc.in), "input %q", tc.in)
	}
}

func TestRedactBeaconAddr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"141.95.98.197:32555", "http://141.95.98.197:32555"},                                          // nothing to mask
		{"https://beacon.example:5052", "https://beacon.example:5052"},                                 // nothing to mask
		{"user:pass@host:5052", "http://user:xxxxx@host:5052"},                                         // basic-auth password
		{"ethereum-beacon.blockpi.network/rpc/v1/KEY", "http://ethereum-beacon.blockpi.network/xxxxx"}, // API key in the path
		{"http://host:5052/?apikey=KEY&network=main", "http://host:5052/xxxxx?apikey=xxxxx&network=xxxxx"},
		{"http://host:5052;user:pass@host2:5052", "http://host:5052;http://user:xxxxx@host2:5052"}, // ";"-separated list
		{"http://user:pass@host:port/KEY", "<malformed>"},                                          // can't be parsed, so masked whole
	} {
		require.Equal(t, tc.want, RedactBeaconAddr(tc.in), "input %q", tc.in)
	}
}

// A malformed address fails client construction with an error that quotes it raw; neither the returned error nor
// the log may carry its credentials.
func TestNew_MalformedAddressOmitsCredentials(t *testing.T) {
	const password, apiKey = "s3cret-password", "s3cret-api-key"

	core, logs := observer.New(zap.DebugLevel)
	_, err := New(t.Context(), zap.New(core), Options{
		BeaconNodeAddr: "http://user:" + password + "@host:port/" + apiKey,
		CommonTimeout:  time.Second,
		LongTimeout:    time.Second,
	})
	require.ErrorContains(t, err, "beacon node address #1: create http client: invalid port")
	require.NotContains(t, err.Error(), password)
	require.NotContains(t, err.Error(), apiKey)

	for _, entry := range logs.All() {
		logged := fmt.Sprint(entry.Message, entry.ContextMap())
		require.NotContains(t, logged, password)
		require.NotContains(t, logged, apiKey)
	}
	require.NotZero(t, logs.FilterMessage("Consensus http client initialization failed").Len())
}
