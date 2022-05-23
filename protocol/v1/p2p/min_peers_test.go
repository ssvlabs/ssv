package protcolp2p

import (
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestWaitForMinPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)
	pi, err := GenPeerID()
	require.NoError(t, err)

	eventHandler := func(e MockMessageEvent) *message.SSVMessage {
		return nil
	}

	sub := NewMockNetwork(logger, pi, 10, eventHandler)
	pk := "a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b"
	vpk, err := hex.DecodeString(pk)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, WaitForMinPeers(ctx, logger, sub, vpk, 2, time.Millisecond*2))
	}()

	pi1, err := GenPeerID()
	require.NoError(t, err)
	sub.AddPeers(vpk, NewMockNetwork(logger, pi1, 10, eventHandler))
	time.After(time.Millisecond * 10)

	pi2, err := GenPeerID()
	require.NoError(t, err)
	sub.AddPeers(vpk, NewMockNetwork(logger, pi2, 10, eventHandler))

	wg.Wait()
}
