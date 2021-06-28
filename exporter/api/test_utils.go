package api

import (
	"encoding/json"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
)

func NewAdapterMock(logger *zap.Logger) WebSocketAdapter {
	adapter := AdapterMock{
		logger: logger,
		In:     make(chan Message),
		Out:    make(chan Message, 3),
	}
	return &adapter
}

type AdapterMock struct {
	logger *zap.Logger
	In     chan Message
	Out    chan Message
}

func (am *AdapterMock) RegisterHandler(mux *http.ServeMux, endPoint string, handler EndPointHandler) {
	am.logger.Warn("not implemented")
}

func (am *AdapterMock) Send(conn Connection, v interface{}) error {
	msg, ok := v.(*Message)
	if !ok {
		return errors.New("fail to cast Message")
	}
	am.Out <- *msg
	return nil
}

func (am *AdapterMock) Receive(conn Connection, v interface{}) error {
	msg := <-am.In
	raw, _ := json.Marshal(&msg)
	return json.Unmarshal(raw, v)
}
