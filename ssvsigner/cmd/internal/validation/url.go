package validation

import (
	"fmt"
	"net"
	"net/url"
)

// ValidateWeb3SignerEndpoint validates that the endpoint is safe from SSRF attacks.
func ValidateWeb3SignerEndpoint(endpoint string) error {
	u, err := url.ParseRequestURI(endpoint)
	if err != nil {
		return fmt.Errorf("invalid url format: %w", err)
	}

	// Only allow http/https schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid url scheme %q: only http/https allowed", u.Scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("missing hostname in url")
	}

	// Parse IP if it's an IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsUnspecified() {
			return fmt.Errorf("invalid ip address type (unspecified): %s", ip)
		}

		if ip.IsMulticast() {
			return fmt.Errorf("invalid ip address type (multicast): %s", ip)
		}

		// Only block non-loopback private IPs
		if !ip.IsLoopback() && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
			return fmt.Errorf("private network ip addresses (excluding loopback) are not allowed: %s", ip)
		}
	}

	return nil
}
