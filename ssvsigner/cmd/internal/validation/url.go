package validation

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// blockedCIDRs contains IP ranges that should be blocked for SSRF prevention.
// Sources: RFC 5735 (IPv4), RFC 6890, and IANA IPv6 Special-Purpose Address Registry.
var blockedCIDRs = []string{
	// IPv4 Private Networks - RFC 1918
	"10.0.0.0/8",     // Private network - RFC 1918
	"172.16.0.0/12",  // Private network - RFC 1918
	"192.168.0.0/16", // Private network - RFC 1918

	// IPv4 Special Use - RFC 5735
	"0.0.0.0/8",          // Current network - RFC 1122
	"169.254.0.0/16",     // Link-local - RFC 3927
	"224.0.0.0/4",        // Multicast - RFC 3171
	"240.0.0.0/4",        // Reserved - RFC 1112
	"255.255.255.255/32", // Broadcast - RFC 919

	// IPv4 Documentation and Test
	"192.0.0.0/24",    // IETF Protocol Assignments - RFC 5736
	"192.0.2.0/24",    // TEST-NET-1 - RFC 5737
	"198.51.100.0/24", // TEST-NET-2 - RFC 5737
	"203.0.113.0/24",  // TEST-NET-3 - RFC 5737
	"192.88.99.0/24",  // IPv6 to IPv4 relay - RFC 3068
	"198.18.0.0/15",   // Network benchmark - RFC 2544
	"100.64.0.0/10",   // Shared Address Space - RFC 6598

	// IPv6 Special Use - IANA Registry
	"::/128",        // Unspecified - RFC 4291
	"fc00::/7",      // Unique local - RFC 4193
	"fe80::/10",     // Link-local - RFC 4291
	"ff00::/8",      // Multicast - RFC 3513
	"100::/64",      // Discard prefix - RFC 6666
	"2001::/23",     // IETF Protocol Assignments - RFC 2928
	"2001:2::/48",   // Benchmarking - RFC 5180
	"2001:db8::/32", // Documentation - RFC 3849
	"2001::/32",     // Teredo tunneling - RFC 4380
	"2002::/16",     // 6to4 - RFC 3056
	"64:ff9b::/96",  // IPv4/IPv6 translation - RFC 6052
	"2001:10::/28",  // Deprecated ORCHID - RFC 4843
	"2001:20::/28",  // ORCHIDv2 - RFC 7343
}

var blockedNetworks []*net.IPNet

func init() {
	for _, cidr := range blockedCIDRs {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil {
			blockedNetworks = append(blockedNetworks, network)
		}
	}
}

// ValidateWeb3SignerEndpoint validates that a Web3Signer endpoint URL is safe from SSRF attacks.
//
// The function performs the following validations:
//  1. URL format and scheme (only http/https allowed)
//  2. Hostname presence
//  3. IP address validation (if hostname is an IP)
//  4. DNS resolution and validation of all resolved IPs (if hostname is a domain)
//
// Allowed endpoints:
//   - Public IP addresses (e.g., https://1.2.3.4:9000)
//   - Localhost/loopback (e.g., http://localhost:9000, http://127.0.0.1:9000)
//   - Domain names resolving to allowed IPs (e.g., https://web3signer.example.com)
//
// Blocked endpoints:
//   - Private networks (e.g., http://192.168.1.1, http://10.0.0.1)
//   - Link-local addresses (e.g., http://169.254.169.254)
//   - Special-use addresses (e.g., http://0.0.0.0, multicast, broadcast)
//   - Non-HTTP(S) schemes (e.g., file://, ftp://)
//
// Returns an error if the endpoint is invalid or potentially unsafe, nil otherwise.
func ValidateWeb3SignerEndpoint(endpoint string, allowInsecureNetworks bool) error {
	u, err := url.ParseRequestURI(endpoint)
	if err != nil {
		return fmt.Errorf("invalid url format: %w", err)
	}

	// Only allow http/https
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid url scheme %q: only http/https allowed", u.Scheme)
	}

	if !allowInsecureNetworks {
		hostname := u.Hostname()
		if hostname == "" {
			return fmt.Errorf("missing hostname in url")
		}

		// Check if it's an IP address
		ip := net.ParseIP(hostname)
		if ip != nil {
			return isBlockedIP(ip)
		}

		// It's a domain name - resolve and validate all IPs
		ips, err := net.LookupIP(hostname)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) {
				if dnsErr.IsNotFound {
					return fmt.Errorf("hostname not found: %s", hostname)
				}

				if dnsErr.IsTimeout {
					return fmt.Errorf("dns lookup timeout for hostname: %s", hostname)
				}

				if dnsErr.IsTemporary {
					return fmt.Errorf("temporary dns failure for hostname: %s", hostname)
				}
			}
			return fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
		}

		if len(ips) == 0 {
			return fmt.Errorf("hostname %s did not resolve to any ip addresses", hostname)
		}

		var blockedIPs []string
		for _, resolvedIP := range ips {
			if err := isBlockedIP(resolvedIP); err != nil {
				blockedIPs = append(blockedIPs, resolvedIP.String())
			}
		}

		if len(blockedIPs) > 0 {
			return fmt.Errorf("hostname %s resolves to blocked ip addresses: %s",
				hostname, strings.Join(blockedIPs, ", "))
		}
	}

	return nil
}

// isBlockedIP checks if an IP address should be blocked based on SSRF prevention rules.
// It returns an error if the IP is in a blocked range, nil otherwise.
//
// The function blocks:
//   - Unspecified addresses (0.0.0.0, ::)
//   - Multicast addresses
//   - Private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7)
//   - Link-local addresses (169.254.0.0/16, fe80::/10)
//   - Special-use addresses (broadcast, documentation, test networks)
//
// The function allows:
//   - Loopback addresses (127.0.0.0/8, ::1) for local Web3Signer support
//   - All public IPv4 and IPv6 addresses
func isBlockedIP(ip net.IP) error {
	// Always block unspecified addresses
	if ip.IsUnspecified() {
		return fmt.Errorf("invalid ip address type (unspecified): %s", ip)
	}

	// Always block multicast
	if ip.IsMulticast() {
		return fmt.Errorf("invalid ip address type (multicast): %s", ip)
	}

	// Allow loopback (localhost)
	if ip.IsLoopback() {
		return nil
	}

	// Check against blocked networks
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("private/internal ip addresses are not allowed: %s", ip)
			}
			return fmt.Errorf("ip address in blocked range: %s", ip)
		}
	}

	return nil
}
