package api

import (
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHandleQuery(t *testing.T) {
	logger := zap.L()
	adapter := adapterMock{
		logger: logger,
		in:     make(chan Message),
		out:    make(chan Message),
	}
	ws := NewWsServer(logger, &adapter, nil).(*wsServer)

	inCn, err := ws.IncomingSubject().Register("TestHandleQuery")
	require.NoError(t, err)
	defer ws.IncomingSubject().Deregister("TestHandleQuery")

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/24")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	go func() {
		for incoming := range inCn {
			nm, ok := incoming.(NetworkMessage)
			require.True(t, ok)
			require.Equal(t, &conn, nm.Conn)
			nm.Msg.Data = []ValidatorMsg{
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
			Filter: MessageFilter{ From: 1 },
		}
		adapter.in <- msg
	}()

	go func() {
		ws.handleQuery(&conn)
	}()

	<- adapter.out
}

func TestHandleStream(t *testing.T) {
	logger := zap.L()
	adapter := adapterMock{
		logger: logger,
		in:     make(chan Message),
		out:    make(chan Message),
	}
	ws := NewWsServer(logger, &adapter, nil).(*wsServer)

	_, ipAddr, err := net.ParseCIDR("192.0.2.1/25")
	require.NoError(t, err)
	conn := connectionMock{addr: ipAddr}

	go func() {
		nm := NetworkMessage{
			Msg:  Message{
				Type: TypeValidator,
				Filter: MessageFilter{From: 0},
				Data: []ValidatorMsg{
					{PublicKey: "pubkey1"},
					{PublicKey: "pubkey2"},
				},
			},
			Err:  nil,
			Conn: nil,
		}
		ws.OutboundSubject().Notify(nm)

		// second message
		time.Sleep(5 * time.Millisecond)
		nm.Msg.Data = []ValidatorMsg{
			{PublicKey: "pubkey3"},
		}
		ws.OutboundSubject().Notify(nm)
		// third message
		time.Sleep(5 * time.Millisecond)
		nm.Msg.Data = []ValidatorMsg{
			{PublicKey: "pubkey4"},
		}
		ws.OutboundSubject().Notify(nm)
	}()

	go func() {
		ws.handleStream(&conn)
	}()

	<- adapter.out
	<- adapter.out
	<- adapter.out
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
	in chan Message
	out chan Message
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
	msg := <- am.in
	raw, _ := json.Marshal(&msg)
	return json.Unmarshal(raw, v)
}
