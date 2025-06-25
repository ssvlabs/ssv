package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWeb3SignerEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{
			name:     "valid https endpoint with real domain",
			endpoint: "https://google.com:443",
			wantErr:  "",
		},
		{
			name:     "valid http endpoint with public IP",
			endpoint: "http://8.8.8.8:9000",
			wantErr:  "",
		},
		{
			name:     "invalid url format",
			endpoint: "invalid-url",
			wantErr:  "invalid url format",
		},
		{
			name:     "localhost allowed",
			endpoint: "http://localhost:9000",
			wantErr:  "",
		},
		{
			name:     "127.0.0.1 allowed",
			endpoint: "http://127.0.0.1:9000",
			wantErr:  "",
		},
		{
			name:     "127.x.x.x allowed",
			endpoint: "http://127.1.2.3:9000",
			wantErr:  "",
		},
		{
			name:     "ipv6 loopback allowed",
			endpoint: "http://[::1]:9000",
			wantErr:  "",
		},
		{
			name:     "private ip 192.168.x.x blocked",
			endpoint: "http://192.168.1.1:9000",
			wantErr:  "private/internal ip addresses are not allowed",
		},
		{
			name:     "private ip 10.x.x.x blocked",
			endpoint: "http://10.0.0.1:9000",
			wantErr:  "private/internal ip addresses are not allowed",
		},
		{
			name:     "private ip 172.16.x.x blocked",
			endpoint: "http://172.16.0.1:9000",
			wantErr:  "private/internal ip addresses are not allowed",
		},
		{
			name:     "link local blocked",
			endpoint: "http://169.254.1.1:9000",
			wantErr:  "private/internal ip addresses are not allowed",
		},
		{
			name:     "file scheme blocked",
			endpoint: "file:///etc/passwd",
			wantErr:  "only http/https allowed",
		},
		{
			name:     "ftp scheme blocked",
			endpoint: "ftp://example.com",
			wantErr:  "only http/https allowed",
		},
		{
			name:     "missing hostname",
			endpoint: "http://",
			wantErr:  "missing hostname in url",
		},
		{
			name:     "unspecified ipv4",
			endpoint: "http://0.0.0.0:9000",
			wantErr:  "invalid ip address type (unspecified)",
		},
		{
			name:     "multicast ip blocked",
			endpoint: "http://224.0.0.1:9000",
			wantErr:  "invalid ip address type (multicast)",
		},
		{
			name:     "non-existent domain",
			endpoint: "https://this-domain-definitely-does-not-exist-12345.com",
			wantErr:  "hostname not found",
		},
		{
			name:     "ipv6 unique local blocked",
			endpoint: "http://[fd00::1]:9000",
			wantErr:  "private/internal ip addresses are not allowed",
		},
		{
			name:     "broadcast address blocked",
			endpoint: "http://255.255.255.255:9000",
			wantErr:  "ip address in blocked range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateWeb3SignerEndpoint(tt.endpoint, "")
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
