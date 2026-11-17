package discovery

import (
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestDiscV5Options_ValidateIP checks that Validate accepts an IPv4, an IPv6 or an empty IP, and rejects a malformed
// one, such as a nil net.IP's string.
func TestDiscV5Options_ValidateIP(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	tests := []struct {
		name    string
		ip      string
		wantErr string
	}{
		{name: "IPv4", ip: "192.0.2.5"},
		{name: "IPv6", ip: "2001:db8::1"},
		{name: "empty", ip: ""},
		{name: "malformed", ip: "<nil>", wantErr: `invalid ip "<nil>"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&DiscV5Options{IP: tt.ip, Port: testPort, NetworkKey: key}).Validate()
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
