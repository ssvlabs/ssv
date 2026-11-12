package commons

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prysmaticlabs/prysm/v4/network"
)

// IPAddr returns the node's local IP: the source address of its IPv4 default route, the one it reaches other
// networks from. Failing that, it's the first address of an up interface (IPv4 first), skipping loopback and
// link-local ones, or 127.0.0.1 if there's none.
func IPAddr() (net.IP, error) {
	return pickIP(defaultRouteSource())
}

// pickIP returns routeSource if it's an IPv4 address peers could reach us at: a private one qualifies, but a
// loopback, link-local, multicast or unspecified one doesn't. Failing that, it falls back as IPAddr describes.
func pickIP(routeSource net.IP) (net.IP, error) {
	if ip := routeSource.To4(); ip.IsGlobalUnicast() {
		return ip, nil
	}
	ip, err := network.ExternalIP()
	if err != nil {
		return nil, fmt.Errorf("could not read interface addresses: %w", err)
	}
	return net.ParseIP(ip), nil
}

// defaultRouteSource returns the source address the OS picks for 192.0.2.1, or nil if it has no route there. That's
// a documentation address (RFC 5737): unless some other route covers it, the default route decides. Dialing UDP
// sends nothing.
func defaultRouteSource() net.IP {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return addr.IP
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
