package api

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

func TestHandleQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancelServerCtx := context.WithCancel(context.Background())
	mux := http.NewServeMux()
	ws := NewWsServer(ctx, func(logger *zap.Logger, nm *NetworkMessage) {
		nm.Msg.Data = []registrystorage.OperatorData{
			{PublicKey: []byte(fmt.Sprintf("pubkey-%d", nm.Msg.Filter.From))},
		}
	}, mux, false).(*wsServer)
	addr := fmt.Sprintf(":%d", getRandomPort(8001, 14000))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		go func() {
			// let the server start
			time.Sleep(100 * time.Millisecond)
			wg.Done()
		}()
		require.NoError(t, ws.Start(logger, addr))
	}()
	wg.Wait()

	clientCtx, cancelClientCtx := context.WithCancel(ctx)
	client := NewWSClient(clientCtx)
	wg.Add(1)
	go func() {
		// sleep so client setup will be finished
		time.Sleep(50 * time.Millisecond)
		go func() {
			// send 2 messages
			defer wg.Done()
			defer cancelClientCtx()
			time.Sleep(10 * time.Millisecond)
			client.out <- Message{
				Type:   TypeOperator,
				Filter: MessageFilter{From: 1, To: 1},
			}
			time.Sleep(10 * time.Millisecond)
			client.out <- Message{
				Type:   TypeOperator,
				Filter: MessageFilter{From: 2, To: 2},
			}
			time.Sleep(10 * time.Millisecond)
		}()
		require.NoError(t, client.StartQuery(logger, addr, "/query"))
	}()

	wg.Wait()
	cancelServerCtx()
	// make sure the connection got 2 responses
	require.Equal(t, 2, client.MessageCount())
	time.Sleep(10 * time.Millisecond)
}

func TestHandleStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	ws := NewWsServer(ctx, nil, mux, false).(*wsServer)
	addr := fmt.Sprintf(":%d", getRandomPort(8001, 14000))
	go func() {
		require.NoError(t, ws.Start(logger, addr))
	}()

	testCtx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	client := NewWSClient(testCtx)
	go func() {
		// sleep so setup will be finished
		time.Sleep(100 * time.Millisecond)
		require.NoError(t, client.StartStream(logger, addr, "/stream"))
	}()

	go func() {
		// sleep so setup will be finished
		time.Sleep(200 * time.Millisecond)
		// sending 3 messages in the stream channel
		ws.out.Send(newTestMessage())

		msg2 := newTestMessage()
		msg2.Data = []registrystorage.OperatorData{
			{PublicKey: []byte("pubkey-operator")},
		}
		ws.out.Send(msg2)

		msg3 := newTestMessage()
		msg3.Type = TypeValidator
		msg3.Data = map[string]string{"PublicKey": "pubkey3"}
		ws.out.Send(msg3)
	}()

	for {
		if client.MessageCount() == 3 {
			return
		}
		time.Sleep(100 * time.Millisecond)
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
