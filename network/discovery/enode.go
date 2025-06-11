package discovery

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"net"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ssvlabs/ssv/network/commons"
)

// createLocalNode create a new enode.LocalNode instance
func createLocalNode(privKey *ecdsa.PrivateKey, storagePath string, ipAddr net.IP, udpPort, tcpPort uint16) (*enode.LocalNode, error) {
	db, err := enode.OpenDB(storagePath)
	if err != nil {
		return nil, fmt.Errorf("could not open node's peer database: %w", err)
	}

	localNode := enode.NewLocalNode(db, privKey)

	localNode.Set(enr.IP(ipAddr))
	localNode.Set(enr.UDP(udpPort))
	localNode.Set(enr.TCP(tcpPort))
	localNode.SetFallbackIP(ipAddr)
	localNode.SetFallbackUDP(int(udpPort))
	localNode.Set(enr.WithEntry("ssv", true))

	return localNode, nil
}

// addAddresses configures node addressing by prioritizing HostDNS over HostAddress.
// Uses resolved DNS IP if available, otherwise falls back to HostAddress.
// Returns error if address resolution fails or addresses are invalid.
func addAddresses(localNode *enode.LocalNode, hostAddr, hostDNS string) error {
	// Try DNS first
	if len(hostDNS) > 0 {
		ips, err := net.LookupIP(hostDNS)
		if err != nil {
			return fmt.Errorf("could not resolve host DNS: %s, %w", hostDNS, err)
		}

		if len(ips) > 0 {
			firstIP := ips[0]
			localNode.SetStaticIP(firstIP)
			localNode.SetFallbackIP(firstIP)

			return nil
		}
	}

	// Use HostAddress as fallback
	if len(hostAddr) > 0 {
		hostIP := net.ParseIP(hostAddr)
		if hostIP == nil || (hostIP.To4() == nil && hostIP.To16() == nil) {
			return fmt.Errorf("invalid host address given: %v", hostAddr)
		}

		localNode.SetStaticIP(hostIP)
		localNode.SetFallbackIP(hostIP)
	}

	return nil
}

// ToPeer creates peer info from the given node
func ToPeer(node *enode.Node) (*peer.AddrInfo, error) {
	m, err := ToMultiAddr(node)
	if err != nil {
		return nil, fmt.Errorf("could not create multiaddr: %w", err)
	}
	pi, err := peer.AddrInfoFromP2pAddr(m)
	if err != nil {
		return nil, fmt.Errorf("could not create peer info: %w", err)
	}
	return pi, nil
}

// PeerID returns the peer id of the node
func PeerID(node *enode.Node) (peer.ID, error) {
	pk, err := commons.ECDSAPubToInterface(node.Pubkey())
	if err != nil {
		return "", err
	}
	return peer.IDFromPublicKey(pk)
}

// ToMultiAddr returns the node's multiaddr.
func ToMultiAddr(node *enode.Node) (ma.Multiaddr, error) {
	id, err := PeerID(node)
	if err != nil {
		return nil, err
	}
	if id.String() == "" {
		return nil, fmt.Errorf("empty peer id")
	}
	ipAddr := node.IP().String()
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil && ip.To16() == nil {
		return nil, fmt.Errorf("invalid ip address: %s", ipAddr)
	}
	port := node.TCP()
	var s string
	if ip.To4() != nil {
		s = fmt.Sprintf("/ip4/%s/%s/%d/p2p/%s", ipAddr, "tcp", port, id.String())
	} else {
		s = fmt.Sprintf("/ip6/%s/%s/%d/p2p/%s", ipAddr, "tcp", port, id.String())
	}
	return ma.NewMultiaddr(s)
}

// ParseENR takes a list of ENR strings and returns
// the corresponding enode.Node objects.
// it also accepts custom schemes, defaults to enode.ValidSchemes (v4)
func ParseENR(schemes enr.SchemeMap, tcpRequired bool, enrs ...string) ([]*enode.Node, error) {
	nodes := make([]*enode.Node, 0)
	if schemes == nil {
		schemes = enode.ValidSchemes
	}
	for _, e := range enrs {
		if e == "" {
			continue
		}
		node, err := enode.Parse(schemes, e)
		if err != nil {
			return nodes, fmt.Errorf("could not parse ENR: %w", err)
		}
		if tcpRequired {
			hasTCP, err := hasTCPEntry(node)
			if err != nil {
				return nil, fmt.Errorf("could not check tcp port: %w", err)
			}
			if !hasTCP {
				return nil, fmt.Errorf("could not find tcp port: %s", e)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// hasTCPEntry ensures that the node has a TCP entry
func hasTCPEntry(node *enode.Node) (bool, error) {
	if err := node.Record().Load(enr.WithEntry(tcp, new(enr.TCP))); err != nil {
		if !enr.IsNotFound(err) {
			return false, fmt.Errorf("could not find tcp port in ENR: %w", err)
		}
		return false, fmt.Errorf("could not load tcp port from ENR: %w", err)
	}
	return true, nil
}

func findNode(nodes []*enode.Node, id enode.ID) *enode.Node {
	for _, node := range nodes {
		if bytes.Equal(node.ID().Bytes(), id.Bytes()) {
			return node
		}
	}
	return nil
}
