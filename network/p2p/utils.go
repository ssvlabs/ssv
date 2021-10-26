package p2p

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	iaddr "github.com/ipfs/go-ipfs-addr"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/io/file"
	"github.com/prysmaticlabs/prysm/network"
	"go.uber.org/zap"
	"net"
	"path/filepath"
	"runtime"
)

func (n *p2pNetwork) parseBootStrapAddrs(addrs []string) (discv5Nodes []string) {
	discv5Nodes, _ = parseGenericAddrs(n.logger, addrs)
	if len(discv5Nodes) == 0 {
		n.logger.Error("No bootstrap addresses supplied")
	}
	return discv5Nodes
}

func parseGenericAddrs(logger *zap.Logger, addrs []string) (enodeString, multiAddrString []string) {
	for _, addr := range addrs {
		if addr == "" {
			// Ignore empty entries
			continue
		}
		_, err := enode.Parse(enode.ValidSchemes, addr)
		if err == nil {
			enodeString = append(enodeString, addr)
			continue
		}
		_, err = multiAddrFromString(addr)
		if err == nil {
			multiAddrString = append(multiAddrString, addr)
			continue
		}
		logger.Error("Invalid address error", zap.String("address", addr), zap.Error(err))
	}
	return enodeString, multiAddrString
}

func multiAddrFromString(address string) (ma.Multiaddr, error) {
	addr, err := iaddr.ParseString(address)
	if err != nil {
		return nil, err
	}
	return addr.Multiaddr(), nil
}

// Retrieves an external ipv4 address and converts into a libp2p formatted value.
func (n *p2pNetwork) ipAddr() net.IP {
	ip, err := network.ExternalIP()
	if err != nil {
		n.logger.Fatal("Could not get IPv4 address", zap.Error(err))
	}
	return net.ParseIP(ip)
}

// Determines a private key for p2p networking from the p2p service's
// configuration struct. If no key is found, it generates a new one.
func privKey() (*ecdsa.PrivateKey, error) {
	defaultKeyPath := defaultDataDir()

	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	rawbytes, err := priv.Raw()
	if err != nil {
		return nil, err
	}
	dst := make([]byte, hex.EncodedLen(len(rawbytes)))
	hex.Encode(dst, rawbytes)
	if err := file.WriteFile(defaultKeyPath, dst); err != nil {
		return nil, err
	}
	convertedKey := convertFromInterfacePrivKey(priv)
	return convertedKey, nil
}

func convertToMultiAddr(logger *zap.Logger, nodes []*enode.Node) []ma.Multiaddr {
	var multiAddrs []ma.Multiaddr
	for _, node := range nodes {
		// ignore nodes with no ip address stored
		if node.IP() == nil {
			logger.Debug("ignore nodes with no ip address stored", zap.String("enr", node.String()))
			continue
		}
		multiAddr, err := convertToSingleMultiAddr(node)
		if err != nil {
			logger.Debug("Could not convert to multiAddr", zap.Error(err))
			continue
		}
		multiAddrs = append(multiAddrs, multiAddr)
	}
	return multiAddrs
}

func convertToSingleMultiAddr(node *enode.Node) (ma.Multiaddr, error) {
	pubkey := node.Pubkey()
	assertedKey := convertToInterfacePubkey(pubkey)
	id, err := peer.IDFromPublicKey(assertedKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not get peer id")
	}
	return multiAddressBuilderWithID(node.IP().String(), tcp, uint(node.TCP()), id)
}

func multiAddressBuilderWithID(ipAddr, protocol string, port uint, id peer.ID) (ma.Multiaddr, error) {
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP.To4() == nil && parsedIP.To16() == nil {
		return nil, errors.Errorf("invalid ip address provided: %s", ipAddr)
	}
	if id.String() == "" {
		return nil, errors.New("empty peer id given")
	}
	if parsedIP.To4() != nil {
		return ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/%s/%d/p2p/%s", ipAddr, protocol, port, id.String()))
	}
	return ma.NewMultiaddr(fmt.Sprintf("/ip6/%s/%s/%d/p2p/%s", ipAddr, protocol, port, id.String()))
}

func multiAddressBuilder(ipAddr string, tcpPort uint) (ma.Multiaddr, error) {
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP.To4() == nil && parsedIP.To16() == nil {
		return nil, errors.Errorf("invalid ip address provided: %s", ipAddr)
	}
	if parsedIP.To4() != nil {
		return ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", ipAddr, tcpPort))
	}
	return ma.NewMultiaddr(fmt.Sprintf("/ip6/%s/tcp/%d", ipAddr, tcpPort))
}

// privKeyOption adds a private key to the libp2p option if the option was provided.
// If the private key file is missing or cannot be read, or if the
// private key contents cannot be marshaled, an exception is thrown.
func privKeyOption(privkey *ecdsa.PrivateKey) libp2p.Option {
	return func(cfg *libp2p.Config) error {
		return cfg.Apply(libp2p.Identity(convertToInterfacePrivkey(privkey)))
	}
}

func convertToInterfacePrivkey(privkey *ecdsa.PrivateKey) crypto.PrivKey {
	typeAssertedKey := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privkey))
	return typeAssertedKey
}

func convertFromInterfacePrivKey(privkey crypto.PrivKey) *ecdsa.PrivateKey {
	typeAssertedKey := (*ecdsa.PrivateKey)(privkey.(*crypto.Secp256k1PrivateKey))
	typeAssertedKey.Curve = gcrypto.S256() // Temporary hack, so libp2p Secp256k1 is recognized as geth Secp256k1 in disc v5.1.
	return typeAssertedKey
}

func convertToInterfacePubkey(pubkey *ecdsa.PublicKey) crypto.PubKey {
	typeAssertedKey := crypto.PubKey((*crypto.Secp256k1PublicKey)(pubkey))
	return typeAssertedKey
}

func convertToAddrInfo(node *enode.Node) (*peer.AddrInfo, ma.Multiaddr, error) {
	multiAddr, err := convertToSingleMultiAddr(node)
	if err != nil {
		return nil, nil, err
	}
	info, err := peer.AddrInfoFromP2pAddr(multiAddr)
	if err != nil {
		return nil, nil, err
	}
	return info, multiAddr, nil
}

// defaultDataDir is the default data directory
func defaultDataDir() string {
	// Try to place the data folder in the user's home dir
	home := file.HomeDir()
	if home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Eth2")
		} else if runtime.GOOS == "windows" {
			return filepath.Join(home, "AppData", "Local", "Eth2")
		} else {
			return filepath.Join(home, ".eth2")
		}
	}
	// As we cannot guess a stable location, return empty and handle later
	return ""
}
