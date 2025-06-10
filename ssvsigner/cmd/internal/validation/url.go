package validation

import (
	"fmt"
	"net"
	"net/url"
	"strings"
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

	// Check for localhost/loopback
	if strings.EqualFold(hostname, "localhost") ||
		strings.HasPrefix(hostname, "127.") ||
		hostname == "::1" {
		return fmt.Errorf("localhost/loopback addresses are not allowed")
	}

	// Parse IP if it's an IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsUnspecified() {
			return fmt.Errorf("invalid ip address type: %s", ip)
		}

		if ip.IsMulticast() {
			return fmt.Errorf("invalid ip address type: %s", ip)
		}

		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("private/local ip addresses are not allowed: %s", ip)
		}
	}

	return nil
}
