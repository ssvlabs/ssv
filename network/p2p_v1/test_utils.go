package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/testing"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
	"time"
)

// LoggerFactory helps to inject loggers
type LoggerFactory func(string) *zap.Logger

// LocalNet holds the nodes in the local network
type LocalNet struct {
	Nodes    []network.V1
	NodeKeys []testing.NodeKeys
	Bootnode *discovery.Bootnode

	udpRand testing.UDPPortsRandomizer
}

func (ln *LocalNet) withBootnode(ctx context.Context, logger *zap.Logger) error {
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
		go func(node network.V1) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(pctx, time.Second*10)
			defer cancel()
			var peers []peer.ID
			for len(peers) < minConnected && ctx.Err() == nil {
				peers = node.(*p2pNetwork).host.Network().Peers()
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

// NewLocalNet creates a new mdns network
func NewLocalNet(ctx context.Context, loggerFactory LoggerFactory, n int, useDiscv5 bool) (*LocalNet, error) {
	ln := &LocalNet{}
	ln.udpRand = make(testing.UDPPortsRandomizer)
	if useDiscv5 {
		if err := ln.withBootnode(ctx, loggerFactory("bootnode")); err != nil {
			return nil, err
		}
	}
	i := 1
	nodes, keys, err := testing.NewLocalNetwork(ctx, n, func(pctx context.Context, keys testing.NodeKeys) network.V1 {
		logger := loggerFactory(fmt.Sprintf("node-%d", i))
		cfg := NewNetConfig(logger, keys.NetKey, &keys.OperatorKey.PublicKey, ln.Bootnode,
			testing.RandomTCPPort(12001, 12999), ln.udpRand.Next(13001, 13999), n)
		p := New(ctx, cfg)
		i++
		err := p.Setup()
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
func NewNetConfig(logger *zap.Logger, netPrivKey *ecdsa.PrivateKey, operatorPubkey *rsa.PublicKey, bn *discovery.Bootnode, tcpPort, udpPort, maxPeers int) *Config {
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
		OperatorPublicKey: operatorPubkey,
		Logger:            logger,
		Fork:              forksv1.New(),
	}
}
