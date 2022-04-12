package protcolp2p

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"sync"
	"testing"
	"time"
)

func TestWaitForMinPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)
	sub := newMockSubscriber()
	pk := "a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b"
	vpk, err := hex.DecodeString(pk)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, WaitForMinPeers(ctx, logger, sub, vpk, 2, time.Millisecond*2))
	}()
	sub.AddPeers(vpk, peer.ID("xxx"))
	time.After(time.Millisecond * 10)
	sub.AddPeers(vpk, peer.ID("xxx-2"))

	wg.Wait()
}

func newMockSubscriber() *mockSubscriber {
	return &mockSubscriber{
		lock:       &sync.Mutex{},
		peers:      make(map[string][]peer.ID),
		subscribed: make(map[string]bool),
	}
}

type mockSubscriber struct {
	lock       sync.Locker
	peers      map[string][]peer.ID
	subscribed map[string]bool
}

func (m mockSubscriber) Subscribe(pk message.ValidatorPK) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	m.subscribed[spk] = true
	return nil
}

func (m mockSubscriber) Unsubscribe(pk message.ValidatorPK) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	delete(m.subscribed, spk)

	return nil
}

func (m mockSubscriber) Peers(pk message.ValidatorPK) ([]peer.ID, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	peers, ok := m.peers[spk]
	if !ok {
		return nil, nil
	}
	return peers, nil
}

// AddPeers adds the given peers as a listeners for the validator
func (m mockSubscriber) AddPeers(pk message.ValidatorPK, added ...peer.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	spk := hex.EncodeToString(pk)
	peers, ok := m.peers[spk]
	if !ok {
		peers = []peer.ID{}
	}
	peers = append(peers, added...)
	m.peers[spk] = peers
}
