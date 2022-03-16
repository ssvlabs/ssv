package discovery

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"net"
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

// decorateLocalNode adds more entries to the ENR
func decorateLocalNode(node *enode.LocalNode, subnets []bool, operatorID string) error {
	var err error
	if len(operatorID) > 0 {
		if err = setOperatorIDEntry(node, operatorID); err != nil {
			return err
		}
	}
	if len(subnets) > 0 {
		err = setSubnetsEntry(node, subnets)
	} else if len(operatorID) > 0 {
		err = setNodeTypeEntry(node, Operator)
	} else {
		err = setNodeTypeEntry(node, Exporter)
	}
	return err
}

// ToPeer creates peer info from the given node
func ToPeer(node *enode.Node) (*peer.AddrInfo, error) {
	m, err := ToMultiAddr(node)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create multiaddr")
	}
	pi, err := peer.AddrInfoFromP2pAddr(m)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create peer info")
	}
	return pi, nil
}

// PeerID returns the peer id of the node
func PeerID(node *enode.Node) (peer.ID, error) {
	pk := crypto.PubKey((*crypto.Secp256k1PublicKey)(node.Pubkey()))
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
