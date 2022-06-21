package peers

import (
	"context"
	"github.com/bloxapp/ssv/utils/tasks"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"go.uber.org/zap"
	"time"
)

// HandleConnections configures a network notifications handler that handshakes and tracks all p2p connections
// note that the node MUST register stream handler for handshake
func HandleConnections(ctx context.Context, logger *zap.Logger, handshaker Handshaker) *libp2pnetwork.NotifyBundle {
	logger = logger.With(zap.String("who", "conn_handler"))

	q := tasks.NewExecutionQueue(time.Millisecond*10, tasks.WithoutErrors())

	go func() {
		c, cancel := context.WithCancel(ctx)
		defer cancel()
		defer q.Stop()
		q.Start()
		<-c.Done()
	}()

	handshake := func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) error {
		id := conn.RemotePeer()
		_logger := logger.With(zap.String("peer", id.String()))
		err := handshaker.Handshake(conn)
		if err != nil {
			// the handshake might have been triggered by the other node,
			// therefore the handshake might be pending
			if err == ErrIndexingInProcess || err == errHandshakeInProcess {
				return nil
			}
			_logger.Warn("could not handshake with peer", zap.Error(err))
			err := net.ClosePeer(id)
			if err != nil {
				_logger.Warn("could not close connection", zap.Error(err))
			}
			return err
		}
		_logger.Debug("new connection is ready",
			zap.String("dir", conn.Stat().Direction.String()))
		metricsConnections.Inc()
		return nil
	}

	return &libp2pnetwork.NotifyBundle{
		ConnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			id := conn.RemotePeer()
			q.QueueDistinct(func() error {
				return handshake(net, conn)
			}, id.String())
		},
		DisconnectedF: func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			if conn == nil || conn.RemoteMultiaddr() == nil {
				return
			}
			// skip if we are still connected to the peer
			if net.Connectedness(conn.RemotePeer()) == libp2pnetwork.Connected {
				return
			}
			metricsConnections.Dec()
		},
		OpenedStreamF: func(network libp2pnetwork.Network, stream libp2pnetwork.Stream) {
			if conn := stream.Conn(); conn != nil {
				metricsStreams.Inc()
			}
		},
		ClosedStreamF: func(network libp2pnetwork.Network, stream libp2pnetwork.Stream) {
			if conn := stream.Conn(); conn != nil {
				metricsStreams.Dec()
			}
		},
	}
}
