package api

import (
	"encoding/json"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
)

// NewAdapterMock creates a new adapter for tests
func NewAdapterMock(logger *zap.Logger) WebSocketAdapter {
	adapter := AdapterMock{
		logger: logger,
		In:     make(chan Message),
		Out:    make(chan Message, 3),
	}
	return &adapter
}

// AdapterMock is a mock websocket adapter
type AdapterMock struct {
	logger *zap.Logger
	In     chan Message
	Out    chan Message
}

// RegisterHandler not implemented
func (am *AdapterMock) RegisterHandler(mux *http.ServeMux, endPoint string, handler EndPointHandler) {
	am.logger.Warn("not implemented")
}

// Send sends the given struct to the out channel
func (am *AdapterMock) Send(conn Connection, v interface{}) error {
	msg, ok := v.(*Message)
	if !ok {
		return errors.New("fail to cast Message")
	}
	am.Out <- *msg
	return nil
}

// Receive reads a struct from the in channel
func (am *AdapterMock) Receive(conn Connection, v interface{}) error {
	msg := <-am.In
	raw, _ := json.Marshal(&msg)
	return json.Unmarshal(raw, v)
}
