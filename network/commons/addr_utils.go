package commons

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/network"
)

// IPAddr returns the external IP address
func IPAddr() (net.IP, error) {
	ip, err := network.ExternalIP()
	if err != nil {
		return nil, errors.Wrap(err, "could not get IPv4 address")
	}
	return net.ParseIP(ip), nil
}

// BuildMultiAddress creates a multiaddr from the given params
func BuildMultiAddress(ipAddr, protocol string, port uint, id peer.ID) (ma.Multiaddr, error) {
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP.To4() == nil && parsedIP.To16() == nil {
		return nil, errors.Errorf("invalid ip address provided: %s", ipAddr)
	}
	maStr := fmt.Sprintf("/ip6/%s/%s/%d", ipAddr, protocol, port)
	if parsedIP.To4() != nil {
		maStr = fmt.Sprintf("/ip4/%s/%s/%d", ipAddr, protocol, port)
	}
	if len(id) > 0 {
		maStr = fmt.Sprintf("%s/p2p/%s", maStr, id.String())
	}
	return ma.NewMultiaddr(maStr)
}

// DedupMultiaddrs returns a copy of addrs with duplicates removed, preserving order.
// Two addresses are considered equal if their multiaddr string representations match.
func DedupMultiaddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	if addrs == nil {
		return nil
	}
	if len(addrs) <= 1 {
		// nothing to deduplicate, just return a copy for consistency
		return append([]ma.Multiaddr(nil), addrs...)
	}
	seen := make(map[string]struct{}, len(addrs))
	out := make([]ma.Multiaddr, 0, len(addrs))
	for _, a := range addrs {
		if a == nil {
			continue
		}
		s := a.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, a)
	}
	return out
}
