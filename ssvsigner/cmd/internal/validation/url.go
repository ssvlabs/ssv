package validation

import (
	"fmt"
	"net/url"
)

// ValidateWeb3SignerEndpoint validates the Web3Signer endpoint URL format.
//
// The function performs minimal validation:
//   - Valid URL format
//   - HTTP/HTTPS scheme (Web3Signer only supports these)
//   - Hostname presence
//
// Everything else is allowed, including:
//   - Private networks (10.x.x.x, 192.168.x.x, etc.) - standard deployment pattern
//   - Localhost/loopback - common for local deployments
//   - Any public IP or domain
//   - Unusual addresses (0.0.0.0, multicast) - will fail naturally at connection time
//
// Returns an error if the URL format is invalid, nil otherwise.
func ValidateWeb3SignerEndpoint(endpoint string) error {
	u, err := url.ParseRequestURI(endpoint)
	if err != nil {
		return fmt.Errorf("invalid url format: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid url scheme %q: only http/https allowed", u.Scheme)
	}

	if u.Hostname() == "" {
		return fmt.Errorf("missing hostname in url")
	}

	return nil
}
