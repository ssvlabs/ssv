package topics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestMsgIDHandlerStartProcessesQueuedEvents(t *testing.T) {
	t.Parallel()

	handler := &msgIDHandler{
		ctx:    t.Context(),
		added:  make(chan addedEvent, 1),
		ids:    make(map[string]*msgIDEntry),
		locker: &sync.Mutex{},
		ttl:    time.Minute,
	}

	handler.Add("queued", peer.ID("peer-1"))
	go handler.Start()

	require.Eventually(t, func() bool {
		handler.locker.Lock()
		defer handler.locker.Unlock()

		entry, ok := handler.ids["queued"]
		return ok && len(entry.peers) == 1 && entry.peers[0] == peer.ID("peer-1")
	}, time.Second, 10*time.Millisecond)
}

func TestMsgIDHandlerAddFallsBackWhenBufferFull(t *testing.T) {
	t.Parallel()

	handler := &msgIDHandler{
		ctx:    context.Background(),
		added:  make(chan addedEvent, 1),
		ids:    make(map[string]*msgIDEntry),
		locker: &sync.Mutex{},
		ttl:    time.Minute,
	}

	handler.added <- addedEvent{mid: "queued", pid: peer.ID("peer-0")}
	handler.Add("fallback", peer.ID("peer-1"))

	handler.locker.Lock()
	defer handler.locker.Unlock()

	entry, ok := handler.ids["fallback"]
	require.True(t, ok)
	require.Equal(t, []peer.ID{peer.ID("peer-1")}, entry.peers)
}
