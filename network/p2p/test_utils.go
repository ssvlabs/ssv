package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/testing"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
	"time"
)

// HostProvider holds host instance
type HostProvider interface {
	Host() host.Host
}

// LoggerFactory helps to inject loggers
type LoggerFactory func(string) *zap.Logger

// LocalNet holds the nodes in the local network
type LocalNet struct {
	Nodes    []network.P2PNetwork
	NodeKeys []testing.NodeKeys
	Bootnode *discovery.Bootnode

	udpRand testing.UDPPortsRandomizer
}

// WithBootnode adds a bootnode to the network
func (ln *LocalNet) WithBootnode(ctx context.Context, logger *zap.Logger) error {
	bnSk, err := commons.GenNetworkKey()
	if err != nil {
		return err
	}
	interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(bnSk))
	b, err := interfacePriv.Raw()
	if err != nil {
		return err
	}
	bn, err := discovery.NewBootnode(ctx, &discovery.BootnodeOptions{
		Logger:     logger.With(zap.String("component", "bootnode")),
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       ln.udpRand.Next(13001, 13999),
	})
	if err != nil {
		return err
	}
	ln.Bootnode = bn
	return nil
}

// CreateAndStartLocalNet creates a new local network and starts it
func CreateAndStartLocalNet(pctx context.Context, loggerFactory LoggerFactory, n, minConnected int, useDiscv5 bool) (*LocalNet, error) {
	ln, err := NewLocalNet(pctx, loggerFactory, n, useDiscv5)
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i, node := range ln.Nodes {
		logger := loggerFactory(fmt.Sprintf("node-%d", i))
		if err := node.Start(); err != nil {
			logger.Error("could not start node", zap.Error(err))
		}
		wg.Add(1)
		go func(node network.P2PNetwork) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(pctx, time.Second*10)
			defer cancel()
			var peers []peer.ID
			for len(peers) < minConnected && ctx.Err() == nil {
				peers = node.(HostProvider).Host().Network().Peers()
				time.Sleep(time.Millisecond * 100)
			}
			if ctx.Err() != nil {
				logger.Fatal("could not find enough peers", zap.Int("n", n), zap.Int("found", len(peers)))
				return
			}
			logger.Debug("found enough peers", zap.Int("n", n), zap.Int("found", len(peers)))
		}(node)
	}

	wg.Wait()

	return ln, nil
}

// NewTestP2pNetwork creates a new network.P2PNetwork instance
func (ln *LocalNet) NewTestP2pNetwork(ctx context.Context, keys testing.NodeKeys, logger *zap.Logger, maxPeers int) (network.P2PNetwork, error) {
	operatorPubkey, err := rsaencryption.ExtractPublicKey(keys.OperatorKey)
	if err != nil {
		return nil, err
	}
	cfg := NewNetConfig(logger, keys.NetKey, operatorPubkey, ln.Bootnode,
		testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), maxPeers)
	p := New(ctx, cfg)
	err = p.Setup()
	if err != nil {
		return nil, err
	}
	return p, nil
}

// NewLocalNet creates a new mdns network
func NewLocalNet(ctx context.Context, loggerFactory LoggerFactory, n int, useDiscv5 bool) (*LocalNet, error) {
	ln := &LocalNet{}
	ln.udpRand = make(testing.UDPPortsRandomizer)
	if useDiscv5 {
		if err := ln.WithBootnode(ctx, loggerFactory("bootnode")); err != nil {
			return nil, err
		}
	}
	i := 0
	nodes, keys, err := testing.NewLocalNetwork(ctx, n, func(pctx context.Context, keys testing.NodeKeys) network.P2PNetwork {
		i++
		logger := loggerFactory(fmt.Sprintf("node-%d", i))
		p, err := ln.NewTestP2pNetwork(pctx, keys, logger, n)
		if err != nil {
			logger.Error("could not setup network", zap.Error(err))
		}
		return p
	})
	if err != nil {
		return nil, err
	}
	ln.NodeKeys = keys
	ln.Nodes = nodes

	<-time.After(time.Millisecond * 500)

	return ln, nil
}

// NewNetConfig creates a new config for tests
func NewNetConfig(logger *zap.Logger, netPrivKey *ecdsa.PrivateKey, operatorID string, bn *discovery.Bootnode, tcpPort, udpPort, maxPeers int) *Config {
	bns := ""
	if bn != nil {
		bns = bn.ENR
	}
	return &Config{
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
		OperatorID:        operatorID,
		Logger:            logger,
		ForkVersion:       forksprotocol.V1ForkVersion,
	}
}
