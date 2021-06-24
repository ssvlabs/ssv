package api

import (
	"github.com/bloxapp/ssv/pubsub"
	"github.com/pkg/errors"
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

	router *http.ServeMux
}

// NewWsServer creates a new instance
func NewWsServer(logger *zap.Logger, adapter WebSocketAdapter, mux *http.ServeMux) WebSocketServer {
	ws := wsServer{
		logger.With(zap.String("component", "exporter/api/server")),
		pubsub.NewSubject(), pubsub.NewSubject(),
		adapter, mux,
	}
	return &ws
}

func (ws *wsServer) Start(addr string) error {
	if ws.adapter == nil {
		return errors.New("websocket adapter is missing")
	}
	ws.adapter.RegisterHandler(ws.router, "/query", ws.handleQuery)
	ws.adapter.RegisterHandler(ws.router, "/stream", ws.handleStream)

	ws.logger.Info("starting websocket server",
		zap.String("addr", addr),
		zap.Strings("endPoints", []string{"/query", "/stream"}))

	err := http.ListenAndServe(addr, ws.router)
	if err != nil {
		ws.logger.Warn("could not start http server", zap.Error(err))
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

	ws.processOutboundForConnection(conn, ConnectionID(conn), true)
}

// handleQuery receives query message and respond async
func (ws *wsServer) handleStream(conn Connection) {
	ws.processOutboundForConnection(conn, ConnectionID(conn), false)
}

func (ws *wsServer) processOutboundForConnection(conn Connection, cid string, once bool) {
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
		ws.logger.Debug("sending outbound",
			zap.String("cid", cid),
			zap.String("msg type", string(nm.Msg.Type)))
		if nm.Conn == conn || nm.Conn == nil {
			err := ws.adapter.Send(conn, nm.Msg)
			if err != nil {
				ws.logger.Error("could not send message", zap.Error(err))
			} else if once {
				break
			}
		}
	}
}
