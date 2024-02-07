package api

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

func CreateNetworkAndSubscribe(t *testing.T, ctx context.Context, options p2pv1.LocalNetOptions, pks ...string) (*p2pv1.LocalNet, []*dummyRouter, error) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	ln, err := p2pv1.CreateAndStartLocalNet(ctx, logger.Named("createNetworkAndSubscribe"), options)
	if err != nil {
		return nil, nil, err
	}
	if len(ln.Nodes) != options.Nodes {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), options.Nodes)
	}

	logger.Debug("created local network")

	routers := make([]*dummyRouter, options.Nodes)
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{
			i: i,
		}
		node.UseMessageRouter(routers[i])
	}

	logger.Debug("subscribing to topics")

	var wg sync.WaitGroup
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			wg.Add(1)
			go func(node network.P2PNetwork, vpk []byte) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, vpk)
		}
	}
	wg.Wait()
	// let the nodes subscribe
	<-time.After(time.Second)
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			peers := make([]peer.ID, 0)
			for len(peers) < 2 {
				peers, err = node.Peers(vpk)
				if err != nil {
					return nil, nil, err
				}
				time.Sleep(time.Millisecond * 100)
			}
		}
	}

	return ln, routers, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(int) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	UseDiscv5                                       bool
	TotalValidators, ActiveValidators, MyValidators int
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ *queue.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

type Node struct {
	HealthyMock atomic.Pointer[error]
}

func (sc *Node) Healthy(context.Context) error {
	err := sc.HealthyMock.Load()
	if err != nil {
		return *err
	}
	return nil
}

func TestRequest(ts *httptest.Server, method, path, token string, body io.Reader) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		return nil, nil, err
	}
	if token != "" {
		req.Header = http.Header{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer " + token},
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	return resp, respBody, nil
}
