package api

import (
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHandleQuery(t *testing.T) {
	logger := zap.L()
	adapter := newAdapterMock(logger)
	ws := NewWsServer(logger, adapter, nil).(*wsServer)

	inCn, err := ws.IncomingSubject().Register("TestHandleQuery")
	require.NoError(t, err)
	defer ws.IncomingSubject().Deregister("TestHandleQuery")

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/24")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	go func() {
		// notify outbound using a bad struct -> should do nothing (except warning log)
		ws.OutboundSubject().Notify(struct{id string}{ "bad-struct" })
	}()

	go func() {
		for incoming := range inCn {
			nm, ok := incoming.(NetworkMessage)
			require.True(t, ok)
			require.Equal(t, &conn, nm.Conn)
			nm.Msg.Data = []ValidatorInformation{
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
		adapter.in <- msg
	}()

	go func() {
		ws.handleQuery(&conn)
	}()

	<-adapter.out
}

func TestHandleStream(t *testing.T) {
	logger := zap.L()
	adapter := newAdapterMock(logger)
	ws := NewWsServer(logger, adapter, nil).(*wsServer)

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/25")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	// expecting outbound messages
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		for {
			<- adapter.out
			wg.Done()
		}
	}()

	go func() {
		// sending 3 messages in the stream channel
		nm := NetworkMessage{
			Msg: Message{
				Type:   TypeValidator,
				Filter: MessageFilter{From: 0},
				Data: []ValidatorInformation{
					{PublicKey: "pubkey1"},
					{PublicKey: "pubkey2"},
				},
			},
			Err:  nil,
			Conn: nil,
		}
		ws.OutboundSubject().Notify(nm)

		time.Sleep(10 * time.Millisecond)
		nm.Msg.Data = []ValidatorInformation{
			{PublicKey: "pubkey3"},
		}
		ws.OutboundSubject().Notify(nm)

		time.Sleep(10 * time.Millisecond)
		nm.Msg.Data = []ValidatorInformation{
			{PublicKey: "pubkey4"},
		}
		ws.OutboundSubject().Notify(nm)
	}()

	go func() {
		ws.handleStream(&conn)
	}()

	wg.Wait()
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

type adapterMock struct {
	logger *zap.Logger
	in     chan Message
	out    chan Message
}

func newAdapterMock(logger *zap.Logger) *adapterMock {
	adapter := adapterMock{
		logger: logger,
		in:     make(chan Message),
		out:    make(chan Message, 3),
	}
	return &adapter
}

func (am *adapterMock) RegisterHandler(mux *http.ServeMux, endPoint string, handler EndPointHandler) {
	am.logger.Warn("not implemented")
}

func (am *adapterMock) Send(conn Connection, v interface{}) error {
	msg, ok := v.(*Message)
	if !ok {
		return errors.New("fail to cast Message")
	}
	am.out <- *msg
	return nil
}

func (am *adapterMock) Receive(conn Connection, v interface{}) error {
	msg := <-am.in
	raw, _ := json.Marshal(&msg)
	return json.Unmarshal(raw, v)
}
