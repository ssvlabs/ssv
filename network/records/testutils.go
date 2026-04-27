package records

import (
	"crypto/ecdsa"
	"fmt"
	"net"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
)

// CreateLocalNode create a new enode.LocalNode instance
func CreateLocalNode(privKey *ecdsa.PrivateKey, storagePath string, ipAddr net.IP, udpPort, tcpPort uint16) (*enode.LocalNode, error) {
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

	return localNode, nil
}
