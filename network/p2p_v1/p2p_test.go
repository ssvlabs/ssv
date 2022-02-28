package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/network/commons"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestP2pNetwork_SetupStart(t *testing.T) {
	threshold.Init()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//logger := zaptest.NewLogger(t)
	logger := zap.L()

	sk, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg := defaultMockConfig(logger, sk)
	p := New(ctx, cfg).(*p2pNetwork)
	require.NoError(t, p.Setup())
	t.Logf("configured first peer %s", p.host.ID().String())
	require.NoError(t, p.Start())
	t.Logf("started first peer %s", p.host.ID().String())
	defer func() {
		_ = p.Close()
	}()

	sk2, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg2 := defaultMockConfig(logger, sk2)
	cfg2.TCPPort++
	cfg2.UDPPort++
	p2 := New(ctx, cfg2).(*p2pNetwork)
	require.NoError(t, p2.Setup())
	t.Logf("configured second peer %s", p2.host.ID().String())
	require.NoError(t, p2.Start())
	t.Logf("started second peer %s", p2.host.ID().String())
	defer func() {
		_ = p2.Close()
	}()

	for p.idx.State(p2.host.ID()) != peers.StateReady {
		<-time.After(time.Second)
	}
	for p2.idx.State(p.host.ID()) != peers.StateReady {
		<-time.After(time.Second)
	}
}

func defaultMockConfig(logger *zap.Logger, sk *ecdsa.PrivateKey) *Config {
	return &Config{
		Bootnodes:         "",
		TCPPort:           commons.DefaultTCP,
		UDPPort:           commons.DefaultUDP,
		HostAddress:       "",
		HostDNS:           "",
		RequestTimeout:    10 * time.Second,
		MaxBatchResponse:  25,
		MaxPeers:          10,
		PubSubTrace:       false,
		NetworkPrivateKey: sk,
		OperatorPublicKey: nil,
		Logger:            logger,
		Fork:              forksv1.New(),
	}
}
