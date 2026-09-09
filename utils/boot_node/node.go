package bootnode

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/prysmaticlabs/prysm/v4/network"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils"
)

// Options contains options to create the node
type Options struct {
	PrivateKey string `yaml:"PrivateKey" env:"BOOT_NODE_PRIVATE_KEY" env-description:"Private key for bootnode identity (generated if empty)"`
	ExternalIP string `yaml:"ExternalIP" env:"BOOT_NODE_EXTERNAL_IP" env-description:"Override bootnode's external IP address"`
	TCPPort    uint16 `yaml:"TcpPort" env:"TCP_PORT" env-description:"TCP port for P2P transport"`
	UDPPort    uint16 `yaml:"UdpPort" env:"UDP_PORT" env-description:"UDP port for discovery"`
	DbPath     string `yaml:"DbPath" env:"BOOT_NODE_DB_PATH" env-description:"Path to bootnode database directory"`
	Network    string `yaml:"Network" env:"NETWORK" env-description:"Ethereum network to connect to"`
}

func (o *Options) ApplyDefaults() {
	o.TCPPort = 5000
	o.UDPPort = 4000
	o.DbPath = "/data/bootnode"
	o.Network = "mainnet"
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
		logger:      logger.Named(log.NameBootNode),
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

// getOnly rejects methods other than GET/HEAD: both endpoints are read-only
// views served on the ENR-advertised TCP port.
func getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func (h *handler) httpHandler() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		write := func(w io.Writer, b []byte) {
			if _, err := w.Write(b); err != nil {
				h.logger.Error("failed to write to HTTP response", zap.Error(err))
			}
		}
		allNodes := h.listener.AllNodes()
		write(w, []byte("Nodes stored in the table:\n"))
		for i, n := range allNodes {
			write(w, fmt.Appendf(nil, "Node %d\n", i))
			write(w, []byte(n.String()+"\n"))
			write(w, []byte("Node ID: "+n.ID().String()+"\n"))
			write(w, []byte("IP: "+n.IP().String()+"\n"))
			write(w, fmt.Appendf(nil, "UDP Port: %d\n", n.UDP()))
			write(w, fmt.Appendf(nil, "TCP Port: %d\n\n", n.TCP()))
		}
	}
}

// Start implements Node interface
func (n *bootNode) Start(ctx context.Context) error {
	privKey, err := utils.ECDSAPrivateKey(n.logger, n.privateKey)
	if err != nil {
		n.logger.Fatal("failed to get p2p private key", zap.Error(err))
	}

	ipAddr, err := network.ExternalIP()
	if err != nil {
		n.logger.Fatal("failed to get external IP", zap.Error(err))
	}

	listener, socketConn := n.createListener(ipAddr, n.discv5port, privKey)
	node := listener.LocalNode().Node()
	n.logger.Info("running",
		zap.Stringer("node", node),
		zap.Stringer("config", n.ssvConfig),
		fields.ProtocolID(n.ssvConfig.DiscoveryProtocolID),
	)

	handler := &handler{
		logger:   n.logger,
		listener: listener,
	}
	health := newBootNodeHealth(n.logger, listener, socketConn)
	health.start(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("/p2p", getOnly(handler.httpHandler()))
	mux.HandleFunc("/healthz", getOnly(health.handler()))

	const timeout = 3 * time.Second

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", n.tcpPort),
		Handler:      mux,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	if err := httpServer.ListenAndServe(); err != nil {
		n.logger.Fatal("failed to start server", zap.Error(err))
	}

	return nil
}

func (n *bootNode) createListener(ipAddr string, port uint16, privateKey *ecdsa.PrivateKey) (discovery.Listener, *discovery.TimedConn) {
	// Create the UDP listener and the LocalNode record.
	ip := net.ParseIP(ipAddr)
	if ip.To4() == nil {
		n.logger.Fatal("IPv4 address not provided", fields.Address(ipAddr))
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
		n.logger.Fatal("valid IP address not provided", fields.Address(ipAddr))
	}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: int(port),
	}
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		n.logger.Fatal("failed to create UDP server", zap.Error(err))
	}
	// Wrap the socket so /healthz can tell whether discv5 is still draining it.
	socketConn := discovery.NewTimedConn(conn)
	localNode, err := n.createLocalNode(privateKey, ip, port)
	if err != nil {
		n.logger.Fatal("failed to create local node", zap.Error(err))
	}

	listener, err := discover.ListenV5(socketConn, localNode, discover.Config{
		PrivateKey:   privateKey,
		V5ProtocolID: &n.ssvConfig.DiscoveryProtocolID,
	})
	if err != nil {
		n.logger.Fatal("failed to create UDPv5 listener", zap.Error(err))
	}

	return listener, socketConn
}

func (n *bootNode) createLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, port uint16) (*enode.LocalNode, error) {
	db, err := enode.OpenDB(filepath.Join(n.dbPath, "enode"))
	if err != nil {
		return nil, fmt.Errorf("could not open node's peer database: %w", err)
	}
	external := net.ParseIP(n.externalIP)
	if n.externalIP == "" {
		external = ipAddr
		n.logger.Info("running with IP", zap.String("ip", ipAddr.String()))
	} else {
		n.logger.Info("running with external IP", zap.String("external_ip", n.externalIP))
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
