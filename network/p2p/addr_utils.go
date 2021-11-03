package p2p

import (
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/network"
	"go.uber.org/zap"
	"net"
	"time"
)

// parseENRs parses the given ENRs
func parseENRs(enrs []string, enforceTCP bool) ([]*enode.Node, error) {
	var nodes []*enode.Node
	for _, item := range enrs {
		if item == "" {
			continue
		}
		node, err := enode.Parse(enode.ValidSchemes, item)
		if err != nil {
			return nil, errors.Wrap(err, "could not parse bootstrap addr")
		}
		if enforceTCP {
			// skip nodes that don't have a TCP entry in their ENR
			if err := node.Record().Load(enr.WithEntry(tcp, new(enr.TCP))); err != nil {
				if !enr.IsNotFound(err) {
					return nil, errors.Wrap(err, "could not retrieve tcp port")
				}
				return nil, errors.Wrap(err, "could not load tcp port")
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// ipAddr returns the external IP address
func ipAddr() (net.IP, error) {
	ip, err := network.ExternalIP()
	if err != nil {
		return nil, errors.Wrap(err, "could not get IPv4 address")
	}
	return net.ParseIP(ip), nil
}

// checkAddress checks that some address is accessible and returns error accordingly
func checkAddress(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, time.Second*10)
	if err != nil {
		return errors.Wrap(err, "IP address is not accessible")
	}
	if err := conn.Close(); err != nil {
		return errors.Wrap(err, "could not close connection")
	}
	return nil
}

// filterInvalidENRs takes a list of ENRs and filter out all invalid records
func filterInvalidENRs(logger *zap.Logger, enrs []string) []string {
	var valid []string
	for _, enr := range enrs {
		if enr == "" {
			// Ignore empty entries
			continue
		}
		_, err := enode.Parse(enode.ValidSchemes, enr)
		if err != nil {
			logger.Error("invalid address error", zap.String("enr", enr), zap.Error(err))
			continue
		}
		valid = append(valid, enr)
	}
	return valid
}

// convertToMultiAddr converts the given enode.Node slice to multi address slice
func convertToMultiAddr(logger *zap.Logger, nodes []*enode.Node) []ma.Multiaddr {
	var multiAddrs []ma.Multiaddr
	for _, node := range nodes {
		// ignore nodes with no ip address stored
		if node.IP() == nil {
			logger.Warn("ignore nodes with no ip address stored", zap.String("enr", node.String()))
			continue
		}
		multiAddr, err := convertToMultiAddrSingle(node)
		if err != nil {
			logger.Warn("could not convert to multiAddr", zap.Error(err))
			continue
		}
		multiAddrs = append(multiAddrs, multiAddr)
	}
	return multiAddrs
}

// convertToMultiAddrSingle converts the given enode.Node to multi address
func convertToMultiAddrSingle(node *enode.Node) (ma.Multiaddr, error) {
	pubkey := node.Pubkey()
	assertedKey := convertToInterfacePubkey(pubkey)
	id, err := peer.IDFromPublicKey(assertedKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not get peer id")
	}
	return buildMultiAddressBuilderWithID(node.IP().String(), tcp, uint(node.TCP()), id)
}

// convertToAddrInfo converts the given enode.Node object to peer.AddrInfo
func convertToAddrInfo(node *enode.Node) (*peer.AddrInfo, error) {
	multiAddr, err := convertToMultiAddrSingle(node)
	if err != nil {
		return nil, err
	}
	info, err := peer.AddrInfoFromP2pAddr(multiAddr)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func buildMultiAddressBuilderWithID(ipAddr, protocol string, port uint, id peer.ID) (ma.Multiaddr, error) {
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

func buildMultiAddress(ipAddr string, tcpPort uint) (ma.Multiaddr, error) {
	parsedIP := net.ParseIP(ipAddr)
	if parsedIP.To4() == nil && parsedIP.To16() == nil {
		return nil, errors.Errorf("invalid ip address provided: %s", ipAddr)
	}
	if parsedIP.To4() != nil {
		return ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", ipAddr, tcpPort))
	}
	return ma.NewMultiaddr(fmt.Sprintf("/ip6/%s/tcp/%d", ipAddr, tcpPort))
}

func udpVersionFromIP(ipAddr net.IP) string {
	if ipAddr.To4() != nil {
		return udp4
	}
	return udp6
}
