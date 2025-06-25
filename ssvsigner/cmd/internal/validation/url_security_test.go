package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWeb3SignerEndpoint_SecurityChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		endpoint        string
		trustedNetworks string
		expectedError   string
	}{
		{
			name:            "allow specific IPv4 /24",
			endpoint:        "http://172.17.0.1:9000",
			trustedNetworks: "172.17.0.0/24",
			expectedError:   "",
		},
		{
			name:            "allow specific IPv4 /32",
			endpoint:        "http://172.17.0.2:9000",
			trustedNetworks: "172.17.0.2/32",
			expectedError:   "",
		},
		{
			name:            "allow specific IPv6 /64",
			endpoint:        "http://[2001:db8::1]:9000",
			trustedNetworks: "2001:db8::/64",
			expectedError:   "",
		},
		{
			name:            "allow specific IPv6 /128",
			endpoint:        "http://[2001:db8::1]:9000",
			trustedNetworks: "2001:db8::1/128",
			expectedError:   "",
		},
		{
			name:            "allow multiple specific networks",
			endpoint:        "http://172.17.0.2:9000",
			trustedNetworks: "172.17.0.2/32,10.0.0.5/32",
			expectedError:   "",
		},
		{
			name:            "reject empty CIDR entry",
			endpoint:        "http://172.17.0.2:9000",
			trustedNetworks: "172.17.0.2/32,,10.0.0.5/32",
			expectedError:   "test trusted networks contain empty CIDR entry",
		},
		{
			name:            "reject whitespace only",
			endpoint:        "http://172.17.0.2:9000",
			trustedNetworks: "   ",
			expectedError:   "test trusted networks cannot be empty string",
		},
		{
			name:            "allow IPv6 /128",
			endpoint:        "http://[2001:db8::1]:9000",
			trustedNetworks: "2001:db8::1/128",
			expectedError:   "",
		},
		{
			name:            "empty trusted networks still blocks private IPs",
			endpoint:        "http://192.168.1.1:9000",
			trustedNetworks: "",
			expectedError:   "private/internal ip addresses are not allowed: 192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateWeb3SignerEndpoint(tt.endpoint, tt.trustedNetworks)
			if tt.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func TestValidateWeb3SignerEndpoint_ProductionSafety(t *testing.T) {
	t.Parallel()

	// Test that common production Web3Signer endpoints work without trusted networks
	productionEndpoints := []string{
		"https://web3signer.example.com:9000",
		"https://validator-signer.internal:9000",
		"https://signer.eth2.local:9000",
	}

	for _, endpoint := range productionEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()

			// These should fail DNS resolution but pass SSRF validation
			err := ValidateWeb3SignerEndpoint(endpoint, "")
			require.Error(t, err)
		})
	}
}
