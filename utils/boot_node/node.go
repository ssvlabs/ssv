package bootnode

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/network"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils"
)

// Options contains options to create the node
type Options struct {
	Logger     *zap.Logger
	PrivateKey string `yaml:"PrivateKey" env:"BOOT_NODE_PRIVATE_KEY" env-description:"boot node private key (default will generate new)"`
	ExternalIP string `yaml:"ExternalIP" env:"BOOT_NODE_EXTERNAL_IP" env-description:"Override boot node's IP' "`
	Network    string `yaml:"Network" env:"NETWORK" env-default:"prater"`
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
	network     core.Network
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	return &bootNode{
		logger:      opts.Logger,
		privateKey:  opts.PrivateKey,
		discv5port:  4000,
		forkVersion: []byte{0x00, 0x00, 0x20, 0x09},
		externalIP:  opts.ExternalIP,
		network:     core.NetworkFromString(opts.Network),
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
	privKey, err := utils.ECDSAPrivateKey(n.logger.With(zap.String("who", "p2pNetworkPrivateKey")), n.privateKey)
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

	// TODO: enable lint (G114: Use of net/http serve function that has no support for setting timeouts (gosec))
	// nolint: gosec
	if err := http.ListenAndServe(fmt.Sprintf(":%d", 5000), mux); err != nil {
		log.Fatalf("Failed to start server %v", err)
	}

	return nil
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

	// TODO(oleg) prysm params params.BeaconConfig().GenesisForkVersion
	// fVersion := params.BeaconConfig().GenesisForkVersion
	fVersion := n.network.ForkVersion()

	// if *forkVersion != "" {
	//	fVersion, err = hex.DecodeString(*forkVersion)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve fork version")
	//	}
	//	if len(fVersion) != 4 {
	//		return nil, errors.Errorf("Invalid fork version size expected %d but got %d", 4, len(fVersion))
	//	}
	//}
	genRoot := [32]byte{}
	// if *genesisValidatorRoot != "" {
	//	retRoot, err := hex.DecodeString(*genesisValidatorRoot)
	//	if err != nil {
	//		return nil, errors.Wrap(err, "Could not retrieve genesis validator root")
	//	}
	//	if len(retRoot) != 32 {
	//		return nil, errors.Errorf("Invalid root size, expected 32 but got %d", len(retRoot))
	//	}
	//	genRoot = bytesutil.ToBytes32(retRoot)
	//}

	// TODO(oleg) used from prysm
	//digest, err := signing.ComputeForkDigest(fVersion, genRoot[:])
	digest, err := ComputeForkDigest(fVersion, genRoot)
	if err != nil {
		return nil, errors.Wrap(err, "Could not compute fork digest")
	}

	forkID := &ENRForkID{
		CurrentForkDigest: digest[:],
		NextForkVersion:   fVersion[:],
		// TODO(oleg) prysm params minimalConfig.FarFutureEpoch = 1<<64 - 1
		//NextForkEpoch:     params.BeaconConfig().FarFutureEpoch,
		NextForkEpoch: 1<<64 - 1,
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

// this returns the 32byte fork data root for the “current_version“ and “genesis_validators_root“.
// This is used primarily in signature domains to avoid collisions across forks/chains.
//
// Spec pseudocode definition:
//
//		def compute_fork_data_root(current_version: Version, genesis_validators_root: Root) -> Root:
//	   """
//	   Return the 32-byte fork data root for the ``current_version`` and ``genesis_validators_root``.
//	   This is used primarily in signature domains to avoid collisions across forks/chains.
//	   """
//	   return hash_tree_root(ForkData(
//	       current_version=current_version,
//	       genesis_validators_root=genesis_validators_root,
//	   ))
func computeForkDataRoot(version phase0.Version, root phase0.Root) ([32]byte, error) {
	r, err := (&phase0.ForkData{
		CurrentVersion:        version,
		GenesisValidatorsRoot: root,
	}).HashTreeRoot()
	if err != nil {
		return [32]byte{}, err
	}
	return r, nil
}

// ComputeForkDigest returns the fork for the current version and genesis validator root
//
// Spec pseudocode definition:
//
//		def compute_fork_digest(current_version: Version, genesis_validators_root: Root) -> ForkDigest:
//	   """
//	   Return the 4-byte fork digest for the ``current_version`` and ``genesis_validators_root``.
//	   This is a digest primarily used for domain separation on the p2p layer.
//	   4-bytes suffices for practical separation of forks/chains.
//	   """
//	   return ForkDigest(compute_fork_data_root(current_version, genesis_validators_root)[:4])
func ComputeForkDigest(version phase0.Version, genesisValidatorsRoot phase0.Root) ([4]byte, error) {
	dataRoot, err := computeForkDataRoot(version, genesisValidatorsRoot)
	if err != nil {
		return [4]byte{}, err
	}
	return ToBytes4(dataRoot[:]), nil
}

// ToBytes4 is a convenience method for converting a byte slice to a fix
// sized 4 byte array. This method will truncate the input if it is larger
// than 4 bytes.
func ToBytes4(x []byte) [4]byte {
	var y [4]byte
	copy(y[:], x)
	return y
}
