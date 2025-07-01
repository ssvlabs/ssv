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
			name:     "private ip 192.168.x.x allowed",
			endpoint: "http://192.168.1.1:9000",
			wantErr:  "",
		},
		{
			name:     "private ip 10.x.x.x allowed",
			endpoint: "http://10.0.0.1:9000",
			wantErr:  "",
		},
		{
			name:     "private ip 172.16.x.x allowed",
			endpoint: "http://172.16.0.1:9000",
			wantErr:  "",
		},
		{
			name:     "link local allowed",
			endpoint: "http://169.254.1.1:9000",
			wantErr:  "",
		},
		{
			name:     "metadata endpoint allowed (will fail at connection)",
			endpoint: "http://169.254.169.254:9000",
			wantErr:  "",
		},
		{
			name:     "unspecified ipv4 allowed (will fail at connection)",
			endpoint: "http://0.0.0.0:9000",
			wantErr:  "",
		},
		{
			name:     "unspecified ipv6 allowed (will fail at connection)",
			endpoint: "http://[::]:9000",
			wantErr:  "",
		},
		{
			name:     "multicast ip allowed",
			endpoint: "http://224.0.0.1:9000",
			wantErr:  "",
		},
		{
			name:     "ipv6 unique local allowed",
			endpoint: "http://[fd00::1]:9000",
			wantErr:  "",
		},
		{
			name:     "broadcast address allowed",
			endpoint: "http://255.255.255.255:9000",
			wantErr:  "",
		},
		{
			name:     "domain that may not exist (validation doesn't check DNS)",
			endpoint: "https://web3signer.internal.company.com:9000",
			wantErr:  "",
		},
		{
			name:     "invalid url format",
			endpoint: "invalid-url",
			wantErr:  "invalid url format",
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
			name:     "empty string",
			endpoint: "",
			wantErr:  "invalid url format",
		},
		{
			name:     "just scheme",
			endpoint: "http:",
			wantErr:  "missing hostname in url",
		},
		{
			name:     "javascript scheme blocked",
			endpoint: "javascript:alert(1)",
			wantErr:  "only http/https allowed",
		},
		{
			name:     "data scheme blocked",
			endpoint: "data:text/html,<script>alert(1)</script>",
			wantErr:  "only http/https allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateWeb3SignerEndpoint(tt.endpoint)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
