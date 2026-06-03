package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestWSSession_Send_FullQueue asserts the broadcasted.Send contract for
// *wsSession: Send must not block, even when called past the queue
// capacity, and on overflow it cancels the session's ctx (the teardown
// signal consumed by WriteLoop / ReadLoop / handleStream defers). It
// also pins the post-cancel-no-op behavior: once the session is
// torn down, further Sends must not enqueue, even if buffer slots are
// available — otherwise WriteLoop's randomized select could drain a
// few more messages to the wire before exiting.
func TestWSSession_Send_FullQueue(t *testing.T) {
	ws := dialTestWebsocket(t)
	sess := newWSSession(t.Context(), ws, "test", 0, false)
	require.NoError(t, sess.ctx.Err(), "ctx should not be canceled before overflow")

	for i := 0; i < chanSize+2; i++ {
		sess.Send([]byte(fmt.Sprintf("test-%d", i)))
	}
	require.Error(t, sess.ctx.Err(), "ctx should be canceled after overflow")

	// Drain a slot so the buffer has room, then verify a post-cancel
	// Send refuses to enqueue.
	<-sess.send
	sess.Send([]byte("post-cancel"))
	require.Equal(t, chanSize-1, len(sess.send), "Send after cancel must not enqueue")
}

// TestPingMsg pins that pingMsg never panics regardless of id length: it
// reads indices 0,1,3,4, so without the length guard any id shorter than 5
// chars (e.g. a withPing=true session built with a short id) would panic.
func TestPingMsg(t *testing.T) {
	tests := []struct {
		name string
		cid  string
		want []byte
	}{
		{"empty", "", []byte{}},
		{"short", "ab", []byte("ab")},
		{"len-4 trap", "test", []byte("test")}, // would panic at cid[4] without the guard
		{"boundary-5", "abcde", []byte{'a', 'b', 'd', 'e'}},
		{"realistic id", "conn-127.0.0.1:54321-1780320736564442000", []byte{'c', 'o', 'n', '-'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				require.Equal(t, tt.want, pingMsg(tt.cid))
			})
		})
	}
}

// dialTestWebsocket spins up an httptest server that upgrades and then idles,
// dials it, and returns the client-side *websocket.Conn. The server and the
// client conn are both torn down via t.Cleanup.
func dialTestWebsocket(t *testing.T) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u websocket.Upgrader
		serverWS, err := u.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer serverWS.Close()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}
