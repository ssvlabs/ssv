package api

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHandleQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx, cancelServerCtx := context.WithCancel(context.Background())
	mux := http.NewServeMux()
	ws := NewWsServer(ctx, logger, func(nm *NetworkMessage) {
		nm.Msg.Data = []storage.OperatorInformation{
			{PublicKey: fmt.Sprintf("pubkey-%d", nm.Msg.Filter.From)},
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
		require.NoError(t, ws.Start(addr))
	}()
	wg.Wait()

	clientCtx, cancelClientCtx := context.WithCancel(ctx)
	client := NewWSClient(clientCtx, logger)
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
		require.NoError(t, client.StartQuery(addr, "/query"))
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
	ws := NewWsServer(ctx, logger, nil, mux, false).(*wsServer)
	addr := fmt.Sprintf(":%d", getRandomPort(8001, 14000))
	go func() {
		require.NoError(t, ws.Start(addr))
	}()

	testCtx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	client := NewWSClient(testCtx, logger)
	go func() {
		// sleep so setup will be finished
		time.Sleep(100 * time.Millisecond)
		require.NoError(t, client.StartStream(addr, "/stream"))
	}()

	go func() {
		// sleep so setup will be finished
		time.Sleep(200 * time.Millisecond)
		// sending 3 messages in the stream channel
		ws.out.Send(newTestMessage())

		msg2 := newTestMessage()
		msg2.Data = []storage.OperatorInformation{
			{PublicKey: "pubkey-operator"},
		}
		ws.out.Send(msg2)

		msg3 := newTestMessage()
		msg3.Type = TypeValidator
		msg3.Data = []storage.ValidatorInformation{
			{PublicKey: "pubkey3"},
		}
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
		Data: []storage.ValidatorInformation{
			{PublicKey: "pubkey1"},
			{PublicKey: "pubkey2"},
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
