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
	"github.com/prysmaticlabs/prysm/v4/network"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils"
)

// Options contains options to create the node
type Options struct {
	PrivateKey string `yaml:"PrivateKey" env:"BOOT_NODE_PRIVATE_KEY" env-description:"Private key for bootnode identity (generated if empty)"`
	ExternalIP string `yaml:"ExternalIP" env:"BOOT_NODE_EXTERNAL_IP" env-description:"Override bootnode's external IP address"`
	TCPPort    uint16 `yaml:"TcpPort" env:"TCP_PORT" env-default:"5000" env-description:"TCP port for P2P transport"`
	UDPPort    uint16 `yaml:"UdpPort" env:"UDP_PORT" env-default:"4000" env-description:"UDP port for discovery"`
	DbPath     string `yaml:"DbPath" env:"BOOT_NODE_DB_PATH" env-default:"/data/bootnode" env-description:"Path to bootnode database directory"`
	Network    string `yaml:"Network" env:"NETWORK" env-default:"mainnet" env-description:"Ethereum network to connect to"`
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
	discv5port  uint16
	forkVersion []byte
	externalIP  string
	tcpPort     uint16
	dbPath      string
	ssvConfig   *networkconfig.SSV
}

// New is the constructor of ssvNode
func New(logger *zap.Logger, ssvConfig *networkconfig.SSV, opts Options) (Node, error) {
	return &bootNode{
		logger:      logger.Named(logging.NameBootNode),
		privateKey:  opts.PrivateKey,
		discv5port:  opts.UDPPort,
		forkVersion: []byte{0x00, 0x00, 0x20, 0x09},
		externalIP:  opts.ExternalIP,
		tcpPort:     opts.TCPPort,
		dbPath:      opts.DbPath,
		ssvConfig:   ssvConfig,
	}, nil
}

type handler struct {
	logger   *zap.Logger
	listener discovery.Listener
}

func (h *handler) httpHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
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
}

// Start implements Node interface
func (n *bootNode) Start(ctx context.Context) error {
	privKey, err := utils.ECDSAPrivateKey(n.logger, n.privateKey)
	if err != nil {
		log.Fatal("Failed to get p2p privateKey", zap.Error(err))
	}

	ipAddr, err := network.ExternalIP()
	// ipAddr = "127.0.0.1"
	log.Print("TEST Ip addr----", ipAddr)
	if err != nil {
		n.logger.Fatal("Failed to get ExternalIP", zap.Error(err))
	}

	listener := n.createListener(ipAddr, n.discv5port, privKey)
	node := listener.LocalNode().Node()
	n.logger.Info("Running",
		zap.Stringer("node", node),
		zap.Stringer("config", n.ssvConfig),
		fields.ProtocolID(n.ssvConfig.DiscoveryProtocolID),
	)

	handler := &handler{
		logger:   n.logger,
		listener: listener,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/p2p", handler.httpHandler())

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

func (n *bootNode) createListener(ipAddr string, port uint16, privateKey *ecdsa.PrivateKey) discovery.Listener {
	// Create the UDP listener and the LocalNode record.
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil {
		n.logger.Fatal("IPV4 address not provided", fields.Address(ipAddr))
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
		n.logger.Fatal("Valid ip address not provided", fields.Address(ipAddr))
	}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: int(port),
	}
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		log.Fatal(err)
	}
	localNode, err := n.createLocalNode(privateKey, ip, port)
	if err != nil {
		log.Fatal(err)
	}

	listener, err := discover.ListenV5(conn, localNode, discover.Config{
		PrivateKey:   privateKey,
		V5ProtocolID: &n.ssvConfig.DiscoveryProtocolID,
	})
	if err != nil {
		log.Fatal(err)
	}

	return listener
}

func (n *bootNode) createLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, port uint16) (*enode.LocalNode, error) {
	db, err := enode.OpenDB(filepath.Join(n.dbPath, "enode"))
	if err != nil {
		return nil, errors.Wrap(err, "Could not open node's peer database")
	}
	external := net.ParseIP(n.externalIP)
	if n.externalIP == "" {
		external = ipAddr
		n.logger.Info("Running with IP", zap.String("ip", ipAddr.String()))
	} else {
		n.logger.Info("Running with External IP", zap.String("external_ip", n.externalIP))
	}

	localNode := enode.NewLocalNode(db, privKey)
	localNode.Set(enr.WithEntry("ssv", true))
	localNode.SetFallbackIP(external)
	localNode.SetFallbackUDP(int(port))

	ipEntry := enr.IP(external)
	udpEntry := enr.UDP(port)
	tcpEntry := enr.TCP(n.tcpPort)

	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)

	return localNode, nil
}
