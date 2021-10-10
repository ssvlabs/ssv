package api

import (
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleQuery(t *testing.T) {
	logger := zap.L()
	adapter := NewAdapterMock(logger).(*AdapterMock)
	ws := NewWsServer(logger, adapter, nil).(*wsServer)

	inCn, err := ws.IncomingSubject().Register("TestHandleQuery")
	require.NoError(t, err)
	defer ws.IncomingSubject().Deregister("TestHandleQuery")

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/24")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	go func() {
		// notify outbound using a bad struct -> should do nothing (except warning log)
		ws.OutboundSubject().Notify(struct{ id string }{"bad-struct"})
	}()

	go func() {
		for incoming := range inCn {
			nm, ok := incoming.(NetworkMessage)
			require.True(t, ok)
			require.Equal(t, &conn, nm.Conn)
			nm.Msg.Data = []storage.ValidatorInformation{
				{PublicKey: "pubkey1"},
				{PublicKey: "pubkey2"},
			}
			ws.OutboundSubject().Notify(nm)
			return
		}
	}()

	go func() {
		time.Sleep(5 * time.Millisecond)
		msg := Message{
			Type:   TypeOperator,
			Filter: MessageFilter{From: 1},
		}
		adapter.In <- msg
	}()

	go func() {
		ws.handleQuery(&conn)
	}()

	<-adapter.Out
}

func TestHandleStream(t *testing.T) {
	msgCount := 3
	logger := zap.L()
	adapter := NewAdapterMock(logger).(*AdapterMock)
	ws := NewWsServer(logger, adapter, nil).(*wsServer)

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/25")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}
	_, ipAddr2, err := net.ParseCIDR("192.0.2.1/26")
	require.NoError(t, err)
	conn2 := connectionMock{addr: ipAddr2}

	// register a listener to count how many messages are passed on outbound subject
	var outCnCount int64
	var wgCn sync.WaitGroup
	wgCn.Add(3)
	go func() {
		sub, ok := ws.OutboundSubject().(pubsub.Subject)
		require.True(t, ok)
		cn, err := sub.Register("xxx-1")
		require.NoError(t, err)
		for range cn {
			atomic.AddInt64(&outCnCount, int64(1))
			wgCn.Done()
		}
	}()
	// registers a dummy lister to mock disconnections
	go func() {
		sub, ok := ws.OutboundSubject().(pubsub.Subject)
		require.True(t, ok)
		cn, err := sub.Register("xxx-2")
		require.NoError(t, err)
		for range cn {
			sub.Deregister("xxx-2")
			return
		}
	}()

	// expecting outbound messages
	var wg sync.WaitGroup
	wg.Add(msgCount)
	go func() {
		i := 0
		for {
			<-adapter.Out
			i++
			if i >= msgCount {
				if i > msgCount {
					t.Error("should not send too many requests")
				}
				wg.Done()
				return
			}
			wg.Done()
		}
	}()

	go func() {
		// sending 3 messages in the stream channel
		nm := NetworkMessage{
			Msg: Message{
				Type:   TypeValidator,
				Filter: MessageFilter{From: 0},
				Data: []storage.ValidatorInformation{
					{PublicKey: "pubkey1"},
					{PublicKey: "pubkey2"},
				},
			},
			Err:  nil,
			Conn: nil,
		}
		ws.OutboundSubject().Notify(nm)

		time.Sleep(10 * time.Millisecond)
		nm.Msg.Data = []storage.OperatorInformation{
			{PublicKey: "pubkey-operator"},
		}
		ws.OutboundSubject().Notify(nm)

		time.Sleep(10 * time.Millisecond)
		nm.Msg.Data = []storage.ValidatorInformation{
			{PublicKey: "pubkey3"},
		}
		ws.OutboundSubject().Notify(nm)
		// let the message propagate
		time.Sleep(10 * time.Millisecond)
	}()

	go func() {
		ws.handleStream(&conn)
		ws.handleStream(&conn2)
	}()

	wg.Wait()
	wgCn.Wait()

	require.Equal(t, int64(msgCount), atomic.LoadInt64(&outCnCount))
}

type connectionMock struct {
	addr net.Addr
}

func (cm *connectionMock) Close() error {
	return nil
}

func (cm *connectionMock) LocalAddr() net.Addr {
	return cm.addr
}
