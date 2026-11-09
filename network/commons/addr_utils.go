package commons

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prysmaticlabs/prysm/v4/network"
)

// IPAddr returns the node's IP address: the source address of the IPv4 default route, which on a host with
// several interfaces is the one it reaches other networks from. Without a default route, it's the first address
// of an interface that is up (IPv4 first, in the order the OS lists them), skipping loopback and link-local ones,
// or 127.0.0.1 if there's none.
func IPAddr() (net.IP, error) {
	if ip := defaultRouteIPv4(); ip != nil {
		return ip, nil
	}
	ip, err := network.ExternalIP()
	if err != nil {
		return nil, fmt.Errorf("could not get IPv4 address: %w", err)
	}
	return net.ParseIP(ip), nil
}

// defaultRouteIPv4 returns the source address of the IPv4 default route, or nil without one. It asks for the route
// to a documentation address (RFC 5737), which no route but the default one covers. Dialing UDP sends nothing.
func defaultRouteIPv4() net.IP {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	ip := conn.LocalAddr().(*net.UDPAddr).IP.To4()
	if !ip.IsGlobalUnicast() {
		return nil
	}
	return ip
}

// BuildMultiAddress creates a multiaddr from the given params
func BuildMultiAddress(ipAddr, protocol string, port uint, id peer.ID) (ma.Multiaddr, error) {
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP.To4() == nil && parsedIP.To16() == nil {
		return nil, fmt.Errorf("invalid ip address provided: %s", ipAddr)
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
