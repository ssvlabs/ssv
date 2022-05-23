package discovery

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network/commons"
	v1_testing "github.com/bloxapp/ssv/network/testing"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 4
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	udpRand := make(v1_testing.UDPPortsRandomizer)
	bn, err := createTestBootnode(ctx, logger, udpRand.Next(13001, 13999))
	require.NoError(t, err)
	keys, err := v1_testing.CreateKeys(n)
	require.NoError(t, err)

	var wg sync.WaitGroup
	peers := make([]host.Host, n)
	for i := 0; i < n; i++ {
		h, err := libp2p.New(ctx,
			libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
		require.NoError(t, err)
		<-time.After(time.Millisecond * 10)
		peers[i] = h
	}
	nodes := make([]*enode.LocalNode, n)
	for i, k := range keys {
		tcpPort := v1_testing.RandomTCPPort(12001, 12999)
		h := peers[i]
		addrs := h.Addrs()
		if len(addrs) > 0 {
			addr := addrs[0]
			tcpPortS, err := addr.ValueForProtocol(multiaddr.P_TCP)
			require.NoError(t, err)
			tcpPort, err = strconv.Atoi(tcpPortS)
			require.NoError(t, err)
		}
		fmt.Printf("using tcp port %d\n", tcpPort)
		node, err := newDiscV5Service(ctx, &Options{
			Logger:    logger.With(zap.Int("i", i)),
			ConnIndex: &mockConnIndex{},
			DiscV5Opts: &DiscV5Options{
				StoragePath: "",
				IP:          "127.0.0.1",
				BindIP:      "0.0.0.0",
				Port:        udpRand.Next(13001, 13999),
				TCPPort:     tcpPort,
				NetworkKey:  k.NetKey,
				Bootnodes:   []string{bn.ENR},
				Logger:      logger.With(zap.Int("i", i), zap.String("who", "dv5")),
			},
		})
		require.NoError(t, err)
		nodes[i] = node.(*DiscV5Service).Self()
		// start and count connected nodes
		wg.Add(1)
		go func() {
			_ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			expected := 3
			found := 0
			go func() {
				err := <-_ctx.Done()
				require.NotEqual(t, context.DeadlineExceeded, err)
				require.GreaterOrEqual(t, found, expected)
			}()
			require.NoError(t, node.Bootstrap(func(e PeerEvent) {
				found++
				logger.Debug("found node", zap.Any("e", e))
				if found >= expected {
					wg.Done()
					return
				}
				if err := node.(*DiscV5Service).dv5Listener.Ping(e.Node); err != nil {
					t.Log("could not ping node", e.Node.ID().String(), "with err:", err.Error())
					return
				}
			}))
		}()
	}
	wg.Wait()
}

func createTestBootnode(ctx context.Context, logger *zap.Logger, port int) (*Bootnode, error) {
	bnSk, err := commons.GenNetworkKey()
	if err != nil {
		return nil, err
	}
	interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(bnSk))
	b, err := interfacePriv.Raw()
	if err != nil {
		return nil, err
	}
	return NewBootnode(ctx, &BootnodeOptions{
		Logger:     logger.With(zap.String("component", "bootnode")),
		PrivateKey: hex.EncodeToString(b),
		ExternalIP: "127.0.0.1",
		Port:       port,
	})
}

type mockConnIndex struct {
}

func (c *mockConnIndex) Connectedness(id peer.ID) libp2pnetwork.Connectedness {
	return libp2pnetwork.NotConnected
}

func (c *mockConnIndex) CanConnect(id peer.ID) bool {
	return true
}

func (c *mockConnIndex) Limit(dir libp2pnetwork.Direction) bool {
	return false
}

func (c *mockConnIndex) IsBad(id peer.ID) bool {
	return false
}
