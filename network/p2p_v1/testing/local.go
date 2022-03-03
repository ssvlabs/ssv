package testing

import (
	"context"
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network/commons"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	p2pv1 "github.com/bloxapp/ssv/network/p2p_v1"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/crypto"
	"go.uber.org/zap"
	"math/rand"
	"net"
	"time"
)

// NodeKeys holds node's keys
type NodeKeys struct {
	NetKey      *ecdsa.PrivateKey
	OperatorKey *rsa.PrivateKey
}

// LocalNet holds the nodes in the local network
type LocalNet struct {
	Nodes    []p2pv1.P2PNetwork
	Bootnode *Bootnode
}

var rsaKeySize = 2048

// CreateKeys creates <n> random node keys
func CreateKeys(n int) ([]NodeKeys, error) {
	identities := make([]NodeKeys, n)
	for i := 0; i < n; i++ {
		netKey, err := commons.GenNetworkKey()
		if err != nil {
			return nil, err
		}
		opKey, err := rsa.GenerateKey(crand.Reader, rsaKeySize)
		if err != nil {
			return nil, err
		}
		identities[i] = NodeKeys{
			NetKey:      netKey,
			OperatorKey: opKey,
		}
	}
	return identities, nil
}

// NewLocalNet creates a new mdns network
func NewLocalNet(ctx context.Context, logger *zap.Logger, identities ...NodeKeys) (*LocalNet, error) {
	ln := &LocalNet{}

	up := make(UDPPortsRand)

	n := len(identities)

	for i, idn := range identities {
		cfg := NewConfig(logger.With(zap.String("component", fmt.Sprintf("node-%d", i))),
			idn.NetKey, &idn.OperatorKey.PublicKey, nil,
			RandomTCPPort(12001, 12999), up.Next(13001, 13999), n)
		p := p2pv1.New(ctx, cfg)
		err := p.Setup()
		if err != nil {
			return nil, err
		}
		ln.Nodes = append(ln.Nodes, p)
		if err = p.Start(); err != nil {
			return nil, err
		}
		<-time.After(time.Millisecond * 100)
	}

	<-time.After(time.Millisecond * 500)

	return ln, nil
}

// NewLocalDiscV5Net creates a new discv5 network
func NewLocalDiscV5Net(ctx context.Context, logger *zap.Logger, identities ...NodeKeys) (*LocalNet, error) {
	ln := &LocalNet{}

	bnSk, err := commons.GenNetworkKey()
	if err != nil {
		return nil, err
	}
	interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(bnSk))
	b, err := interfacePriv.Raw()
	if err != nil {
		return nil, err
	}
	bn, err := NewBootnode(ctx, &Options{
		Logger:     logger.With(zap.String("component", "bootnode")),
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       RandomTCPPort(13001, 13999),
	})
	if err != nil {
		return nil, err
	}
	ln.Bootnode = bn
	// let the bootnode start
	<-time.After(time.Millisecond * 500)

	up := make(UDPPortsRand)
	n := len(identities)

	for i, idn := range identities {
		cfg := NewConfig(logger.With(zap.String("component", fmt.Sprintf("node-%d", i))),
			idn.NetKey, &idn.OperatorKey.PublicKey, bn,
			RandomTCPPort(12001, 12999),
			up.Next(13001, 13999), n)
		p := p2pv1.New(ctx, cfg)
		if err = p.Setup(); err != nil {
			return nil, err
		}
		ln.Nodes = append(ln.Nodes, p)
		if err = p.Start(); err != nil {
			return nil, err
		}
	}
	// wait for node to find other peers
	// TODO: do it w/o timeout by asking the nodes for found peers
	<-time.After(time.Millisecond * 500)

	return ln, nil
}

// NewConfig creates a new config for tests
func NewConfig(logger *zap.Logger, netPrivKey *ecdsa.PrivateKey, operatorPubkey *rsa.PublicKey, bn *Bootnode, tcpPort, udpPort, maxPeers int) *p2pv1.Config {
	bns := ""
	if bn != nil {
		bns = bn.ENR
	}
	return &p2pv1.Config{
		Bootnodes:         bns,
		TCPPort:           tcpPort,
		UDPPort:           udpPort,
		HostAddress:       "",
		HostDNS:           "",
		RequestTimeout:    10 * time.Second,
		MaxBatchResponse:  25,
		MaxPeers:          maxPeers,
		PubSubTrace:       false,
		NetworkPrivateKey: netPrivKey,
		OperatorPublicKey: operatorPubkey,
		Logger:            logger,
		Fork:              forksv1.New(),
	}
}

// RandomTCPPort returns a new random tcp port
func RandomTCPPort(from, to int) int {
	for {
		port := random(from, to)
		if checkTCPPort(port) == nil {
			// port is taken
			continue
		}
		return port
	}
}

// checkTCPPort checks that the given port is not taken
func checkTCPPort(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), 3*time.Second)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// UDPPortsRand helps to generate random, available udp ports
type UDPPortsRand map[int]bool

// Next generates a new random port that is available
func (up UDPPortsRand) Next(from, to int) int {
	udpPort := random(from, to)
udpPortLoop:
	for {
		if !up[udpPort] {
			up[udpPort] = true
			break udpPortLoop
		}
		udpPort = random(from, to)
	}
	return udpPort
}

func random(from, to int) int {
	// #nosec G404
	return rand.Intn(to-from) + from
	//n, err := crand.Int(crand.Reader, big.NewInt(int64(to-from)))
	//if err != nil {
	//	return 0
	//}
	//return int(n.Int64()) + from
}

// CreateShares creates n shares
func CreateShares(n int) []*bls.SecretKey {
	threshold.Init()

	var res []*bls.SecretKey
	for i := 0; i < n; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		res = append(res, &sk)
	}
	return res
}
