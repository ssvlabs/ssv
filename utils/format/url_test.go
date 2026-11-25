package format

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://relay.example.com:18550", "https://relay.example.com:18550"},                             // nothing to mask
		{"https://0xabcd@relay.example.com", "https://0xabcd@relay.example.com"},                           // a username alone is kept
		{"http://user:pass@host:5052", "http://user:xxxxx@host:5052"},                                      // basic-auth password
		{"https://builder.example.com/rpc/v1/KEY", "https://builder.example.com/xxxxx"},                    // API key in the path
		{"http://host:5052/?apikey=KEY&network=main", "http://host:5052/xxxxx?apikey=xxxxx&network=xxxxx"}, // query values
		{"http://user:pass@host:port/KEY", "<malformed>"},                                                  // can't be parsed, so masked whole
	} {
		require.Equal(t, tc.want, RedactURL(tc.in), "input %q", tc.in)
	}
}
