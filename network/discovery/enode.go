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
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/network/commons"
)

// createLocalNode create a new enode.LocalNode instance
func createLocalNode(privKey *ecdsa.PrivateKey, storagePath string, ipAddr net.IP, udpPort, tcpPort int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB(storagePath)
	if err != nil {
		return nil, errors.Wrap(err, "could not open node's peer database")
	}
	localNode := enode.NewLocalNode(db, privKey)

	localNode.Set(enr.IP(ipAddr))
	localNode.Set(enr.UDP(udpPort))
	localNode.Set(enr.TCP(tcpPort))
	localNode.SetFallbackIP(ipAddr)
	localNode.SetFallbackUDP(udpPort)

	return localNode, nil
}

// addAddresses adds configured address and/or dns if configured
func addAddresses(localNode *enode.LocalNode, hostAddr, hostDNS string) error {
	if len(hostAddr) > 0 {
		hostIP := net.ParseIP(hostAddr)
		if hostIP.To4() == nil && hostIP.To16() == nil {
			return fmt.Errorf("invalid host address given: %s", hostIP.String())
		}
		localNode.SetFallbackIP(hostIP)
		localNode.SetStaticIP(hostIP)
	}
	if len(hostDNS) > 0 {
		ips, err := net.LookupIP(hostDNS)
		if err != nil {
			return errors.Wrap(err, "could not resolve host address")
		}
		if len(ips) > 0 {
			firstIP := ips[0]
			localNode.SetFallbackIP(firstIP)
		}
	}
	return nil
}

// ToPeer creates peer info from the given node
func ToPeer(node *enode.Node) (*peer.AddrInfo, error) {
	m, err := ToMultiAddr(node)
	if err != nil {
		return nil, errors.Wrap(err, "could not create multiaddr")
	}
	pi, err := peer.AddrInfoFromP2pAddr(m)
	if err != nil {
		return nil, errors.Wrap(err, "could not create peer info")
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
		return nil, errors.New("empty peer id")
	}
	ipAddr := node.IP().String()
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil && ip.To16() == nil {
		return nil, errors.Errorf("invalid ip address: %s", ipAddr)
	}
	port := uint(node.TCP())
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
			return nodes, errors.Wrap(err, "could not parse ENR")
		}
		if tcpRequired {
			hasTCP, err := hasTCPEntry(node)
			if err != nil {
				return nil, errors.Wrap(err, "could not check tcp port")
			}
			if !hasTCP {
				return nil, errors.Wrap(err, "could not find tcp port")
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
			return false, errors.Wrap(err, "could not find tcp port in ENR")
		}
		return false, errors.Wrap(err, "could not load tcp port from ENR")
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
