package discovery

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/network/commons"
	v1_testing "github.com/bloxapp/ssv/network/p2p_v1/testing"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"testing"
)

func TestNewService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 4
	udpRand := make(v1_testing.UDPPortsRandomizer)
	bn, err := createTestBootnode(ctx, zaptest.NewLogger(t), udpRand.Next(13001, 13999))
	require.NoError(t, err)
	keys, err := v1_testing.CreateKeys(n)
	require.NoError(t, err)

	//var wg sync.WaitGroup
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	nodes := make([]*enode.LocalNode, n)
	for i, k := range keys {
		node, err := newDiscV5Service(ctx, &Options{
			Logger:    logger.With(zap.Int("i", i)),
			ConnIndex: &mockConnIndex{},
			DiscV5Opts: &DiscV5Options{
				StoragePath: "",
				IP:          "127.0.0.1",
				BindIP:      "0.0.0.0",
				Port:        udpRand.Next(13001, 13999),
				TCPPort:     0,
				NetworkKey:  k.NetKey,
				Bootnodes:   []string{bn.ENR},
				Logger:      logger.With(zap.Int("i", i), zap.String("who", "dv5")),
			},
		})
		require.NoError(t, err)
		nodes[i] = node.(*DiscV5Service).Self()
		// TODO: fix and unmark
		//wg.Add(1)
		//go func() {
		//defer wg.Done()
		//found := 0
		//go func() {
		//	require.NoError(t, node.Bootstrap(func(e PeerEvent) {
		//		if err := node.(*DiscV5Service).dv5Listener.Ping(e.Node); err != nil {
		//			t.Log("could not ping node", e.Node.ID().String(), "with err:", err.Error())
		//			return
		//		}
		//		t.Log("pinged node", e.Node.ID().String())
		//		found++
		//	}))
		//}()
		//_ctx, cancel := context.WithTimeout(ctx, time.Second * 5)
		//defer cancel()
		//for _ctx.Err() == nil {
		//	if found > n/2 {
		//		return
		//	}
		//	time.Sleep(time.Millisecond * 100)
		//}
		//require.Nil(t, _ctx.Err(), "found only %d nodes", found)
		//}()
	}
	//wg.Wait()
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

func (c *mockConnIndex) Connected(id peer.ID) bool {
	return false
}

func (c *mockConnIndex) Limit(dir libp2pnetwork.Direction) bool {
	return false
}

func (c *mockConnIndex) IsBad(id peer.ID) bool {
	return false
}
