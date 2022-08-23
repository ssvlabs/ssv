package records

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"net"
)

// CreateLocalNode create a new enode.LocalNode instance
func CreateLocalNode(privKey *ecdsa.PrivateKey, storagePath string, ipAddr net.IP, udpPort, tcpPort int) (*enode.LocalNode, error) {
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
