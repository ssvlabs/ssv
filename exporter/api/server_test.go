package api

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

func TestHandleQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancelServerCtx := context.WithCancel(t.Context())
	defer cancelServerCtx()
	mux := http.NewServeMux()
	ws := NewWsServer(ctx, zap.NewNop(), func(nm *NetworkMessage) {
		nm.Msg.Data = []registrystorage.OperatorData{
			{PublicKey: fmt.Sprintf("pubkey-%d", nm.Msg.Filter.From)},
		}
	}, mux, false).(*wsServer)
	port := getRandomPort(8001, 14000)
	addr := fmt.Sprintf(":%d", port)
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- ws.Start(addr)
	}()
	waitForCondition(t, 2*time.Second, func() bool { return checkPort(port) == nil })
	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
		t.Fatal("server exited unexpectedly")
	default:
	}

	clientCtx, cancelClientCtx := context.WithCancel(ctx)
	client := NewWSClient(clientCtx, logger)
	clientErrCh := make(chan error, 1)
	go func() {
		clientErrCh <- client.StartQuery(addr, "/query")
	}()

	client.out <- Message{
		Type:   TypeOperator,
		Filter: MessageFilter{From: 1, To: 1},
	}
	client.out <- Message{
		Type:   TypeOperator,
		Filter: MessageFilter{From: 2, To: 2},
	}
	cancelClientCtx()

	select {
	case err := <-clientErrCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for query client to finish")
	}

	// make sure the connection got 2 responses
	require.Equal(t, 2, client.MessageCount())
}

func TestHandleStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background() // t.Context() breaks the test
	mux := http.NewServeMux()
	ws := NewWsServer(ctx, zap.NewNop(), nil, mux, false).(*wsServer)
	port := getRandomPort(8001, 14000)
	addr := fmt.Sprintf(":%d", port)
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- ws.Start(addr)
	}()
	waitForCondition(t, 2*time.Second, func() bool { return checkPort(port) == nil })
	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
		t.Fatal("server exited unexpectedly")
	default:
	}

	testCtx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()

	client := NewWSClient(testCtx, logger)
	clientErrCh := make(chan error, 1)
	go func() {
		clientErrCh <- client.StartStream(addr, "/stream")
	}()

	// Wait until one stream client is connected and registered.
	waitForCondition(t, 2*time.Second, func() bool {
		b := ws.broadcaster.(*broadcaster)
		b.mut.Lock()
		defer b.mut.Unlock()
		return len(b.connections) == 1
	})
	select {
	case err := <-clientErrCh:
		require.NoError(t, err)
		t.Fatal("stream client exited unexpectedly")
	default:
	}

	// send 3 messages in the stream channel
	ws.out.Send(newTestMessage())

	msg2 := newTestMessage()
	msg2.Data = []registrystorage.OperatorData{
		{PublicKey: "pubkey-operator"},
	}
	ws.out.Send(msg2)

	msg3 := newTestMessage()
	msg3.Type = TypeValidator
	msg3.Data = map[string]string{"PublicKey": "pubkey3"}
	ws.out.Send(msg3)

	waitForCondition(t, 2*time.Second, func() bool { return client.MessageCount() == 3 })

	cancelCtx()
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition not met before timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestMessage() Message {
	return Message{
		Type:   TypeValidator,
		Filter: MessageFilter{From: 0},
		Data: []map[string]string{
			{"PublicKey": "pubkey1"},
			{"PublicKey": "pubkey3"},
		},
	}
}

func getRandomPort(from, to int) int {
	for {
		port := rand.Intn(to-from) + from
		if checkPort(port) == nil {
			// port is taken
			continue
		}
		return port
	}
}

func checkPort(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), 3*time.Second)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}
