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

// TestConn_Send_FullQueue asserts the broadcasted.Send contract for *conn:
// Send must not block, even when called past the queue capacity, and on
// overflow it cancels the conn ctx (the teardown signal consumed by
// WriteLoop / ReadLoop / handleStream defers).
func TestConn_Send_FullQueue(t *testing.T) {
	ws := dialTestWebsocket(t)
	c := newConn(t.Context(), ws, "test", 0, false)
	require.NoError(t, c.ctx.Err(), "ctx should not be canceled before overflow")

	for i := 0; i < chanSize+2; i++ {
		c.Send([]byte(fmt.Sprintf("test-%d", i)))
	}
	require.Error(t, c.ctx.Err(), "ctx should be canceled after overflow")
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
