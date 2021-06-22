package api

import (
	"github.com/bloxapp/ssv/pubsub"
	"go.uber.org/zap"
	"net/http"
)

// wsServer is an implementation of WebSocketServer
type wsServer struct {
	logger *zap.Logger
	// inbound is a subject to wrap incoming requests
	inbound pubsub.Subject
	// outbound is a subject for writing messages
	outbound pubsub.Subject

	adapter WebSocketAdapter
}

// NewWsServer creates a new instance
func NewWsServer(logger *zap.Logger, adapter WebSocketAdapter) WebSocketServer {
	l := logger.With(zap.String("component", "wsServer"))
	ws := wsServer{l, pubsub.NewSubject(), pubsub.NewSubject(), adapter}
	return &ws
}

func (ws *wsServer) Start(addr string) error {
	ws.adapter.RegisterHandler("/query", ws.handleQuery)
	ws.adapter.RegisterHandler("/stream", ws.handleStream)

	ws.logger.Info("starting a websocket server", zap.String("addr", addr))

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		ws.logger.Warn("could not start http server", zap.Error(err))
	} else {
		ws.logger.Info("exporter endpoints (ws) are ready",
			zap.String("addr", addr),
			zap.Strings("endPoints", []string{"/query", "/stream"}))
	}
	return err
}

func (ws *wsServer) IncomingSubject() pubsub.Subscriber {
	return ws.inbound
}

func (ws *wsServer) OutboundSubject() pubsub.Publisher {
	return ws.outbound
}

// handleQuery receives query message and respond async
func (ws *wsServer) handleQuery(conn Connection) {
	var nm NetworkMessage
	var incoming Message
	err := ws.adapter.Receive(conn, &incoming)
	if err != nil {
		ws.logger.Warn("could not read incoming message", zap.Error(err))
		nm = NetworkMessage{incoming, err, conn}
	} else {
		nm = NetworkMessage{incoming, nil, conn}
	}
	ws.inbound.Notify(nm)

	ws.processOutboundForConnection(conn, ConnectionID(conn))
}

// handleQuery receives query message and respond async
func (ws *wsServer) handleStream(conn Connection) {
	ws.processOutboundForConnection(conn, ConnectionID(conn))
}

func (ws *wsServer) processOutboundForConnection(conn Connection, cid string) {
	out, err := ws.outbound.Register(cid)
	if err != nil {
		ws.logger.Error("could not register outbound subject",
			zap.Error(err), zap.String("cid", cid))
	}
	defer ws.outbound.Deregister(cid)

	for m := range out {
		nm, ok := m.(NetworkMessage)
		if !ok {
			ws.logger.Warn("could not parse message")
			continue
		}
		if nm.Conn == conn || nm.Conn == nil {
			err := ws.adapter.Send(conn, &nm.Msg)
			if err != nil {
				ws.logger.Error("could not send message", zap.Error(err))
			}
		}
	}
}
