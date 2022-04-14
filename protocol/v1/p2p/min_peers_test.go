package protcolp2p

import (
	"context"
	"encoding/hex"
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
	pi, err := GenPeerID()
	require.NoError(t, err)
	sub := NewMockNetwork(logger, pi, 10)
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
	sub.AddPeers(vpk, NewMockNetwork(logger, pi1, 10))
	time.After(time.Millisecond * 10)

	pi2, err := GenPeerID()
	require.NoError(t, err)
	sub.AddPeers(vpk, NewMockNetwork(logger, pi2, 10))

	wg.Wait()
}
