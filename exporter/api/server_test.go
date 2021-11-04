package api

import (
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleQuery(t *testing.T) {
	logger := zap.L()
	adapter := NewAdapterMock(logger).(*AdapterMock)

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/24")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	ws := NewWsServer(logger, adapter, func(nm *NetworkMessage) {
		require.Equal(t, &conn, nm.Conn)
		nm.Msg.Data = []storage.OperatorInformation{
			{PublicKey: "pubkey1"},
			{PublicKey: "pubkey2"},
		}
	}, nil).(*wsServer)

	go func() {
		ws.handleQuery(&conn)
	}()

	go func() {
		time.Sleep(5 * time.Millisecond)
		msg := Message{
			Type:   TypeOperator,
			Filter: MessageFilter{From: 1},
		}
		adapter.In <- msg
	}()

	<-adapter.Out
}

func TestHandleStream(t *testing.T) {
	msgCount := 3
	logger := zaptest.NewLogger(t)
	adapter := NewAdapterMock(logger).(*AdapterMock)
	ws := NewWsServer(logger, adapter, nil, nil).(*wsServer)

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/25")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}
	go ws.handleStream(&conn)

	_, ipAddr2, err := net.ParseCIDR("192.0.2.1/26")
	require.NoError(t, err)
	conn2 := connectionMock{addr: ipAddr2}
	go ws.handleStream(&conn2)

	sub, ok := ws.OutboundSubject().(pubsub.EventSubscriber)
	require.True(t, ok)
	// register a listener to count how many messages are passed on outbound subject
	var outCnCount int64
	var wgCn sync.WaitGroup
	wgCn.Add(3)
	cn1, done1 := sub.Channel("out")
	defer done1()
	go func() {
		for range cn1 {
			atomic.AddInt64(&outCnCount, int64(1))
			wgCn.Done()
		}
	}()
	cn2, done2 := sub.Channel("out")
	// registers a dummy listener that de-registers
	go func() {
		defer done2()
		for range cn2 {
			return
		}
	}()
	// expecting outbound messages
	var wg sync.WaitGroup
	wg.Add(msgCount * 2)
	go func() {
		i := 0
		for {
			<-adapter.Out
			i++
			if i >= msgCount*2 {
				if i > msgCount*2 {
					t.Error("should not send too many requests")
				}
				wg.Done()
				return
			}
			wg.Done()
		}
	}()

	go func() {
		// sleep so setup will be finished
		time.Sleep(10 * time.Millisecond)

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
		go ws.OutboundSubject().Notify("out", nm)

		nm.Msg.Data = []storage.OperatorInformation{
			{PublicKey: "pubkey-operator"},
		}
		go ws.OutboundSubject().Notify("out", nm)
		nm.Msg.Data = []storage.ValidatorInformation{
			{PublicKey: "pubkey3"},
		}
		ws.OutboundSubject().Notify("out", nm)
		// let the message propagate
		time.Sleep(10 * time.Millisecond)
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
