package commons

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prysmaticlabs/prysm/v4/network"
)

// IPAddr returns the node's local IP: the source address of its IPv4 default route, the one it reaches other
// networks from. Failing that, it's the first address of an up interface (IPv4 first, in OS order), skipping
// loopback and link-local ones, or 127.0.0.1 if there's none.
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

// defaultRouteIPv4 returns the source address of the IPv4 default route, or nil if there's no usable one. It looks
// up the route to a documentation address (RFC 5737), which no local network covers. Dialing UDP sends nothing.
func defaultRouteIPv4() net.IP {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	// Rule out an unusable source: loopback, link-local or unspecified. Private addresses pass IsGlobalUnicast.
	ip := addr.IP.To4()
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
