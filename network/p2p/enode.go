package p2p

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"go.uber.org/zap"
	"net"
)

type discv5Listener interface {
	Self() *enode.Node
	Close()
	Lookup(enode.ID) []*enode.Node
	Resolve(*enode.Node) *enode.Node
	RandomNodes() enode.Iterator
	Ping(*enode.Node) error
	RequestENR(*enode.Node) (*enode.Node, error)
	LocalNode() *enode.LocalNode
}

// createExtendedLocalNode creates
func (n *p2pNetwork) createExtendedLocalNode() (*enode.LocalNode, error) {
	ipAddr := n.ipAddr()
	privKey := n.privKey
	operatorPubKey, err := n.getOperatorPubKey()
	if err != nil {
		return nil, err
	}
	localNode, err := createBaseLocalNode(
		privKey,
		ipAddr,
		n.cfg.UDPPort,
		n.cfg.TCPPort,
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not create Local node")
	}
	if len(operatorPubKey) > 0 {
		localNode, err = withOperatorPubKeyEntry(localNode, []byte(pubKeyHash(operatorPubKey)))
		if err != nil {
			return nil, errors.Wrap(err, "could not create public key entry")
		}
	}
	if len(n.cfg.HostAddress) > 0 {
		hostIP := net.ParseIP(n.cfg.HostAddress)
		if hostIP.To4() == nil && hostIP.To16() == nil {
			n.logger.Error("Invalid host address given", zap.String("hostIp", hostIP.String()))
		} else {
			n.logger.Info("using external IP", zap.String("IP from config", n.cfg.HostAddress), zap.String("IP", hostIP.String()))
			localNode.SetFallbackIP(hostIP)
			localNode.SetStaticIP(hostIP)
		}
	}
	if len(n.cfg.HostDNS) > 0 {
		_host := n.cfg.HostDNS
		ips, err := net.LookupIP(_host)
		if err != nil {
			return nil, errors.Wrap(err, "could not resolve host address")
		}
		if len(ips) > 0 {
			// Use first IP returned from the
			// resolver.
			firstIP := ips[0]
			n.logger.Info("using DNS IP", zap.String("DNS", n.cfg.HostDNS), zap.String("IP", firstIP.String()))
			localNode.SetFallbackIP(firstIP)
		}
	}
	return localNode, nil
}

// createBaseLocalNode creates a plain local node
func createBaseLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, udpPort, tcpPort int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB("")
	if err != nil {
		return nil, errors.Wrap(err, "could not open node's peer database")
	}
	localNode := enode.NewLocalNode(db, privKey)

	ipEntry := enr.IP(ipAddr)
	udpEntry := enr.UDP(udpPort)
	tcpEntry := enr.TCP(tcpPort)
	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)
	localNode.SetFallbackIP(ipAddr)
	localNode.SetFallbackUDP(udpPort)

	return withAttSubnets(localNode), nil
}

// withAttSubnets initializes a bitvector of attestation subnets beacon nodes is subscribed to
// and creates a new ENR entry with its default value.
func withAttSubnets(node *enode.LocalNode) *enode.LocalNode {
	bitV := bitfield.NewBitvector64()
	entry := enr.WithEntry("attnets", bitV.Bytes())
	node.Set(entry)
	return node
}

// withOperatorPubKeyEntry adds 'pk' entry to the node.
// pk entry contains the sha256 (hex encoded) of the operator public key
func withOperatorPubKeyEntry(node *enode.LocalNode, pkHash []byte) (*enode.LocalNode, error) {
	bitL, err := bitfield.NewBitlist64FromBytes(64, pkHash)
	if err != nil {
		return node, err
	}
	entry := enr.WithEntry("pk", bitL.ToBitlist())
	node.Set(entry)
	return node, nil
}

// Parses the attestation subnets ENR entry in a node and extracts its value
// as a bitvector for further manipulation.
func extractOperatorPubKeyEntry(record *enr.Record) ([]byte, error) {
	bitL := bitfield.NewBitlist(64)
	entry := enr.WithEntry("pk", &bitL)
	err := record.Load(entry)
	if err != nil {
		return nil, err
	}
	return bitL.Bytes(), nil
}
