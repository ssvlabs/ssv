package p2p

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/p2p/discover"
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

// listenUDP starts a UDP server
func (n *p2pNetwork) listenUDP(ipAddr net.IP) (*net.UDPConn, error) {
	// BindIP is used to specify the ip
	// on which we will bind our listener on
	// by default we will listen to all interfaces.
	var bindIP net.IP
	switch udpVersionFromIP(ipAddr) {
	case udp4:
		bindIP = net.IPv4zero
	case udp6:
		bindIP = net.IPv6zero
	default:
		return nil, errors.New("invalid ip provided")
	}

	//// If Local ip is specified then use that instead.
	//if s.cfg.LocalIP != "" {
	//	ipAddr = net.ParseIP(s.cfg.LocalIP)
	//	if ipAddr == nil {
	//		return nil, errors.New("invalid Local ip provided")
	//	}
	//	bindIP = ipAddr
	//}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: n.cfg.UDPPort,
	}
	// Listen to all network interfaces
	// for both ip protocols.
	networkVersion := "udp"
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	return conn, nil
}

// createDiscV5Listener creates a new discv5 service
func (n *p2pNetwork) createDiscV5Listener() (*discover.UDPv5, *enode.LocalNode, error) {
	privKey := n.privKey
	ipAddr := n.ipAddr()
	conn, err := n.listenUDP(ipAddr)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not listen to UDP")
	}
	localNode, err := n.createExtendedLocalNode()
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not create local node")
	}

	dv5Cfg := discover.Config{
		PrivateKey: privKey,
	}

	dv5Cfg.Bootnodes, err = parseENRs(n.cfg.Discv5BootStrapAddr)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not read bootstrap addresses")
	}

	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not listen to discV5")
	}
	return listener, localNode, nil
}

// createExtendedLocalNode creates an extended enode.LocalNode with all the needed entries to be part of its enr
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
	n.setNodeFallbackAddress(localNode, n.cfg.HostAddress)
	n.setNodeFallbackDNS(localNode, n.cfg.HostDNS)
	return localNode, nil
}


func (n *p2pNetwork) setNodeFallbackAddress(localNode *enode.LocalNode, addr string) {
	if len(addr) == 0 {
		return
	}
	hostIP := net.ParseIP(addr)
	if hostIP.To4() == nil && hostIP.To16() == nil {
		n.logger.Error("invalid host address", zap.String("addr", hostIP.String()))
	} else {
		localNode.SetFallbackIP(hostIP)
		localNode.SetStaticIP(hostIP)
	}
}


func (n *p2pNetwork) setNodeFallbackDNS(localNode *enode.LocalNode, host string) {
	if len(host) == 0 {
		return
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		n.logger.Error("could not resolve host dns", zap.String("addr", host))
	} else if len(ips) > 0 {
		// Use first IP returned from the resolver
		firstIP := ips[0]
		localNode.SetFallbackIP(firstIP)
	}
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
