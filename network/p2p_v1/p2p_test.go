package p2pv1

import (
	"context"
	"crypto/ecdsa"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

func TestP2pNetwork_SetupStart(t *testing.T) {
	threshold.Init()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sk, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg := defaultMockConfig(zaptest.NewLogger(t), sk)
	p := New(ctx, cfg)
	require.NoError(t, p.Setup())
	require.NoError(t, p.Start())
	defer func() {
		_ = p.Close()
	}()

	sk2, err := commons.GenNetworkKey()
	require.NoError(t, err)
	cfg2 := defaultMockConfig(zaptest.NewLogger(t), sk2)
	cfg2.TCPPort++
	cfg2.UDPPort++
	p2 := New(ctx, cfg2)
	require.NoError(t, p2.Setup())
	require.NoError(t, p2.Start())
	defer func() {
		_ = p2.Close()
	}()

	<-time.After(5 * time.Second)
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
		//Fork:              nil,
	}
}
