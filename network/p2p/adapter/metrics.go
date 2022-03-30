package adapter

import (
	p2p "github.com/bloxapp/ssv/network/p2p"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
)

func (n *netV0Adapter) reportAllPeers() {
	pids := n.host.Network().Peers()
	var ids []string
	for _, pid := range pids {
		ids = append(ids, pid.String())
		n.reportPeerIdentity(pid)
	}
	n.logger.Debug("connected peers status",
		zap.Int("count", len(ids)))
	p2p.MetricsAllConnectedPeers.Set(float64(len(ids)))
}

func (n *netV0Adapter) reportTopics() {
	topics := n.topicsCtrl.Topics()
	nTopics := len(topics)
	n.logger.Debug("connected topics",
		zap.Int("count", nTopics))
	for _, name := range topics {
		n.reportTopicPeers(name)
	}
}

func (n *netV0Adapter) reportTopicPeers(name string) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		n.logger.Warn("could not get topic peers", zap.String("topic", name), zap.Error(err))
		return
	}
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	p2p.MetricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

func (n *netV0Adapter) reportPeerIdentity(pid peer.ID) {
	identity, err := n.idx.Identity(pid)
	if err != nil {
		//n.trace("WARNING: could not report peer", zap.String("peer", pid.String()), zap.Error(err))
		return
	}
	n.logger.Debug("peer identity", zap.String("peer", pid.String()),
		zap.String("oid", identity.OperatorID))

	p2p.MetricsPeersIdentity.WithLabelValues(identity.OperatorID, identity.NodeVersion(),
		pid.String(), identity.NodeType()).Set(1)
}

//func reportLastMsg(pid string) {
//	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
//func (n *netV0Adapter) reportPeerIdentity(pid peer.ID) {
//	identity, err := n.idx.Identity(pid)
//	if err != nil {
//		//n.trace("WARNING: could not report peer", zap.String("peer", pid.String()), zap.Error(err))
//		return
//	}
//	n.logger.Debug("peer identity", zap.String("peer", pid.String()),
//		zap.String("oid", identity.OperatorID))
//
//	metricsPeersIdentity.WithLabelValues(identity.OperatorID, identity.NodeVersion(),
//		pid.String(), identity.NodeType()).Set(1)
//}
//
////func reportLastMsg(pid string) {
////	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
////}
////
////func timestamp() int64 {
////	return time.Now().UnixNano() / int64(time.Millisecond)
////}
