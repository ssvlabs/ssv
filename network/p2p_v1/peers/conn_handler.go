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

	q.Start()
	go func() {
		defer q.Stop()
		for ctx.Err() == nil {
			// wait for context done
		}
	}()

	handshake := func(net libp2pnetwork.Network, conn libp2pnetwork.Conn) error {
		id := conn.RemotePeer()
		_logger := logger.With(zap.String("peer", id.String()))
		err := handshaker.Handshake(conn)
		if err != nil {
			if err == ErrIndexingInProcess {
				// TODO: check if that's even possible as we run this function in a
				// 	distinct queue thus the same peer can't here at the same time
				_logger.Debug("could not handshake with peer: already in process")
			}
			_logger.Warn("could not handshake with peer", zap.Error(err))
			if err := net.ClosePeer(id); err != nil {
				_logger.Warn("could not close connection", zap.Error(err))
				return err
			}
		}
		_logger.Debug("successful handshake with new connection",
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
