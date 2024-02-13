package bootnode

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/v4/network"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/utils"
)

// Options contains options to create the node
type Options struct {
	PrivateKey string `yaml:"PrivateKey" env:"BOOT_NODE_PRIVATE_KEY" env-description:"boot node private key (default will generate new)"`
	ExternalIP string `yaml:"ExternalIP" env:"BOOT_NODE_EXTERNAL_IP" env-description:"Override boot node's external IP"`
	TCPPort    int    `yaml:"TcpPort" env:"TCP_PORT" env-default:"5000" env-description:"TCP port for p2p transport"`
	UDPPort    int    `yaml:"UdpPort" env:"UDP_PORT" env-default:"4000" env-description:"UDP port for discovery"`
	DbPath     string `yaml:"DbPath" env:"BOOT_NODE_DB_PATH" env-default:"/data/bootnode" env-description:"Path to the boot node's database"`
	Network    string `yaml:"Network" env:"NETWORK" env-default:"mainnet"`
}

// Node represents the behavior of boot node
type Node interface {
	// Start starts the SSV node
	Start(ctx context.Context, logger *zap.Logger) error
}

// bootNode implements Node interface
type bootNode struct {
	privateKey  string
	discv5port  int
	forkVersion []byte
	externalIP  string
	tcpPort     int
	dbPath      string
	network     networkconfig.NetworkConfig
}

// New is the constructor of ssvNode
func New(opts Options) (Node, error) {
	networkConfig, err := networkconfig.GetNetworkConfigByName(opts.Network)
	if err != nil {
		return nil, err
	}
	return &bootNode{
		privateKey:  opts.PrivateKey,
		discv5port:  opts.UDPPort,
		forkVersion: []byte{0x00, 0x00, 0x20, 0x09},
		externalIP:  opts.ExternalIP,
		tcpPort:     opts.TCPPort,
		dbPath:      opts.DbPath,
		network:     networkConfig,
	}, nil
}

type handler struct {
	listener *discover.UDPv5
}

func (h *handler) httpHandler(logger *zap.Logger) func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		write := func(w io.Writer, b []byte) {
			if _, err := w.Write(b); err != nil {
				logger.Error("Failed to write to http response", zap.Error(err))
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
}

// Start implements Node interface
func (n *bootNode) Start(ctx context.Context, logger *zap.Logger) error {
	logger = logger.Named(logging.NameBootNode)
	privKey, err := utils.ECDSAPrivateKey(logger, n.privateKey)
	if err != nil {
		log.Fatal("Failed to get p2p privateKey", zap.Error(err))
	}
	cfg := discover.Config{
		PrivateKey: privKey,
	}
	ipAddr, err := network.ExternalIP()
	// ipAddr = "127.0.0.1"
	log.Print("TEST Ip addr----", ipAddr)
	if err != nil {
		logger.Fatal("Failed to get ExternalIP", zap.Error(err))
	}
	listener := n.createListener(logger, ipAddr, n.discv5port, cfg)
	node := listener.Self()
	logger.Info("Running", zap.String("node", node.String()))

	handler := &handler{
		listener: listener,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/p2p", handler.httpHandler(logger))

	const timeout = 3 * time.Second

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", n.tcpPort),
		Handler:      mux,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server %v", err)
	}

	return nil
}

func (n *bootNode) createListener(logger *zap.Logger, ipAddr string, port int, cfg discover.Config) *discover.UDPv5 {
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil {
		logger.Fatal("IPV4 address not provided", fields.Address(ipAddr))
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
		logger.Fatal("Valid ip address not provided", fields.Address(ipAddr))
	}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: port,
	}
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		log.Fatal(err)
	}
	localNode, err := n.createLocalNode(logger, cfg.PrivateKey, ip, port)
	if err != nil {
		log.Fatal(err)
	}

	network, err := discover.ListenV5(conn, localNode, cfg)
	if err != nil {
		log.Fatal(err)
	}
	return network
}

func (n *bootNode) createLocalNode(logger *zap.Logger, privKey *ecdsa.PrivateKey, ipAddr net.IP, port int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB(filepath.Join(n.dbPath, "enode"))
	if err != nil {
		return nil, errors.Wrap(err, "Could not open node's peer database")
	}
	external := net.ParseIP(n.externalIP)
	if n.externalIP == "" {
		external = ipAddr
		logger.Info("Running with IP", zap.String("ip", ipAddr.String()))
	} else {
		logger.Info("Running with External IP", zap.String("external-ip", n.externalIP))
	}

	fVersion := n.network.ForkVersion()

	// if *forkVersion != "" {
	//	fVersion, err = hex.DecodeString(*forkVersion)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve fork version")
	//	}
	//	if len(fVersion) != 4 {
	//		return nil, errors.Errorf("InvalidSecret fork version size expected %d but got %d", 4, len(fVersion))
	//	}
	//}
	genRoot := [32]byte{}
	// if *genesisValidatorRoot != "" {
	//	retRoot, err := hex.DecodeString(*genesisValidatorRoot)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve genesis validator root")
	//	}
	//	if len(retRoot) != 32 {
	//		return nil, errors.Errorf("InvalidSecret root size, expected 32 but got %d", len(retRoot))
	//	}
	//	genRoot = bytesutil.ToBytes32(retRoot)
	//}

	digest, err := goclient.ComputeForkDigest(fVersion, genRoot)
	if err != nil {
		return nil, errors.Wrap(err, "Could not compute fork digest")
	}

	forkID := &ENRForkID{
		CurrentForkDigest: digest[:],
		NextForkVersion:   fVersion[:],
		NextForkEpoch:     goclient.FarFutureEpoch,
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
	tcpEntry := enr.TCP(n.tcpPort)

	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)

	return localNode, nil
}
