package bootnode

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/iputils"
	"github.com/prysmaticlabs/prysm/shared/params"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/crypto"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"go.uber.org/zap"
)

// Options contains options to create the node
type Options struct {
	Logger     *zap.Logger
	PrivateKey string
	ExternalIP string
}

// Node represents the behavior of boot node
type Node interface {
	// Start starts the SSV node
	Start(ctx context.Context) error
}

// bootNode implements Node interface
type bootNode struct {
	logger      *zap.Logger
	privateKey  string
	discv5port  int
	forkVersion []byte
	externalIP  string
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	return &bootNode{
		logger:      opts.Logger,
		privateKey:  opts.PrivateKey,
		discv5port:  4000,
		forkVersion: []byte{0x00, 0x00, 0x20, 0x09},
		externalIP:  opts.ExternalIP,
	}
}

type handler struct {
	listener *discover.UDPv5
	logger   *zap.Logger
}

func (h *handler) httpHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	write := func(w io.Writer, b []byte) {
		if _, err := w.Write(b); err != nil {
			h.logger.Error("Failed to write to http response", zap.Error(err))
		}
	}
	allNodes := h.listener.AllNodes()
	write(w, []byte("Nodes stored in the table:\n"))
	for i, n := range allNodes {
		write(w, []byte(fmt.Sprintf("Node %d\n", i)))
		write(w, []byte(n.String()+"\n"))
		write(w, []byte("Node ID: "+n.ID().String()+"\n"))
		write(w, []byte("IP: "+n.IP().String()+"\n"))
		write(w, []byte(fmt.Sprintf("UDP Port: %d", n.UDP())+"\n"))
		write(w, []byte(fmt.Sprintf("TCP Port: %d", n.TCP())+"\n\n"))
	}
}

// Start implements Node interface
func (n *bootNode) Start(ctx context.Context) error {
	privKey := n.extractPrivateKey()
	cfg := discover.Config{
		PrivateKey: privKey,
	}
	ipAddr, err := iputils.ExternalIP()
	//ipAddr = "127.0.0.1"
	log.Print("TEST Ip addr----", ipAddr)
	if err != nil {
		n.logger.Fatal("Failed to get ExternalIP", zap.Error(err))
	}
	listener := n.createListener(ipAddr, n.discv5port, cfg)
	node := listener.Self()
	n.logger.Info("Running bootnode", zap.String("node", node.String()))

	handler := &handler{
		listener: listener,
		logger:   n.logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/p2p", handler.httpHandler)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", 5000), mux); err != nil {
		log.Fatalf("Failed to start server %v", err)
	}

	return nil
}

func (n *bootNode) extractPrivateKey() *ecdsa.PrivateKey {
	var privKey *ecdsa.PrivateKey
	if n.privateKey != "" {
		dst, err := hex.DecodeString(n.privateKey)
		if err != nil {
			panic(err)
		}
		unmarshalledKey, err := crypto.UnmarshalSecp256k1PrivateKey(dst)
		if err != nil {
			panic(err)
		}
		privKey = (*ecdsa.PrivateKey)(unmarshalledKey.(*crypto.Secp256k1PrivateKey))

	} else {
		privInterfaceKey, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
		if err != nil {
			panic(err)
		}
		privKey = (*ecdsa.PrivateKey)(privInterfaceKey.(*crypto.Secp256k1PrivateKey))
		n.logger.Warn("No private key was provided. Using default/random private key")
		b, err := privInterfaceKey.Raw()
		if err != nil {
			panic(err)
		}
		n.logger.Debug("Private Key generated", zap.ByteString("private-key", b))
	}
	privKey.Curve = gcrypto.S256()

	return privKey
}

func (n *bootNode) createListener(ipAddr string, port int, cfg discover.Config) *discover.UDPv5 {
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil {
		n.logger.Fatal("IPV4 address not provided", zap.String("ipAddr", ipAddr))
	}
	var bindIP net.IP
	var networkVersion string
	switch {
	case ip.To16() != nil && ip.To4() == nil:
		bindIP = net.IPv6zero
		networkVersion = "udp6"
	case ip.To4() != nil:
		bindIP = net.IPv4zero
		networkVersion = "udp4"
	default:
		n.logger.Fatal("Valid ip address not provided", zap.String("ipAddr", ipAddr))
	}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: port,
	}
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		log.Fatal(err)
	}
	localNode, err := n.createLocalNode(cfg.PrivateKey, ip, port)
	if err != nil {
		log.Fatal(err)
	}

	network, err := discover.ListenV5(conn, localNode, cfg)
	if err != nil {
		log.Fatal(err)
	}
	return network
}

func (n *bootNode) createLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, port int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB("")
	if err != nil {
		return nil, errors.Wrap(err, "Could not open node's peer database")
	}
	external := net.ParseIP(n.externalIP)
	if n.externalIP == "" {
		external = ipAddr
		n.logger.Info("Running with IP", zap.String("ip", ipAddr.String()))
	} else {
		n.logger.Info("Running with External IP", zap.String("external-ip", n.externalIP))
	}

	//fVersion := params.BeaconConfig().GenesisForkVersion
	fVersion := params.BeaconConfig().GenesisForkVersion

	//if *forkVersion != "" {
	//	fVersion, err = hex.DecodeString(*forkVersion)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve fork version")
	//	}
	//	if len(fVersion) != 4 {
	//		return nil, errors.Errorf("Invalid fork version size expected %d but got %d", 4, len(fVersion))
	//	}
	//}
	genRoot := [32]byte{}
	//if *genesisValidatorRoot != "" {
	//	retRoot, err := hex.DecodeString(*genesisValidatorRoot)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve genesis validator root")
	//	}
	//	if len(retRoot) != 32 {
	//		return nil, errors.Errorf("Invalid root size, expected 32 but got %d", len(retRoot))
	//	}
	//	genRoot = bytesutil.ToBytes32(retRoot)
	//}
	digest, err := helpers.ComputeForkDigest(fVersion, genRoot[:])
	if err != nil {
		return nil, errors.Wrap(err, "Could not compute fork digest")
	}

	forkID := &pb.ENRForkID{
		CurrentForkDigest: digest[:],
		NextForkVersion:   fVersion,
		NextForkEpoch:     params.BeaconConfig().FarFutureEpoch,
	}
	forkEntry, err := forkID.MarshalSSZ()
	if err != nil {
		return nil, errors.Wrap(err, "Could not marshal fork id")
	}

	localNode := enode.NewLocalNode(db, privKey)
	localNode.Set(enr.WithEntry("eth2", forkEntry))
	localNode.Set(enr.WithEntry("attnets", bitfield.NewBitvector64()))
	localNode.SetFallbackIP(external)
	localNode.SetFallbackUDP(port)

	ipEntry := enr.IP(external)
	udpEntry := enr.UDP(port)
	tcpEntry := enr.TCP(5000)

	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)

	return localNode, nil
}
