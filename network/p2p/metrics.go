package p2pv1

import (
	"log"
	"strconv"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils/format"
)

var (
	// MetricsAllConnectedPeers counts all connected peers
	MetricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_p2p_all_connected_peers",
		Help: "Count connected peers",
	})
	// MetricsConnectedPeers counts connected peers for a topic
	MetricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_p2p_connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	// MetricsPeersIdentity tracks peers identity
	MetricsPeersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity",
		Help: "Peers identity",
	}, []string{"pubKey", "operatorID", "operatorName", "v", "pid", "type"})
	metricsRouterIncoming = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:router:in",
		Help: "Counts incoming messages",
	}, []string{"identifier", "mt"})
)

func init() {
	if err := prometheus.Register(MetricsAllConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsPeersIdentity); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsRouterIncoming); err != nil {
		log.Println("could not register prometheus collector")
	}
}

var unknown = "unknown"

func (n *p2pNetwork) reportAllPeers() {
	pids := n.host.Network().Peers()
	n.logger.Debug("connected peers status",
		zap.Int("count", len(pids)))
	MetricsAllConnectedPeers.Set(float64(len(pids)))
}

func (n *p2pNetwork) reportPeerIdentities() {
	pids := n.host.Network().Peers()
	for _, pid := range pids {
		n.reportPeerIdentity(pid)
	}
}

func (n *p2pNetwork) reportTopics() {
	topics := n.topicsCtrl.Topics()
	nTopics := len(topics)
	n.logger.Debug("connected topics",
		zap.Int("count", nTopics))
	for _, name := range topics {
		n.reportTopicPeers(name)
	}
}

func (n *p2pNetwork) reportTopicPeers(name string) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		n.logger.Warn("could not get topic peers", zap.String("topic", name), zap.Error(err))
		return
	}
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	MetricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

func (n *p2pNetwork) reportPeerIdentity(pid peer.ID) {
	n.logger.Info("collecting info to report peer identity")
	opPubKey, opIndex, opName, forkv, nodeVersion, nodeType := unknown, unknown, unknown, unknown, unknown, unknown
	ni, err := n.idx.GetNodeInfo(pid)
	if err == nil && ni != nil {
		opPubKey = unknown
		nodeVersion = unknown
		forkv = ni.ForkVersion.String()
		if ni.Metadata != nil {
			opPubKey = ni.Metadata.OperatorID
			nodeVersion = ni.Metadata.NodeVersion
		}
		nodeType = "operator"
		if len(opPubKey) == 0 && nodeVersion != unknown {
			nodeType = "exporter"
		}
	}

	operatorData, found, opDataErr := n.nodeStorage.GetOperatorDataByPubKey(opPubKey)
	if opDataErr == nil && found {
		opIndex = strconv.FormatUint(operatorData.Index, 10)
		opName = operatorData.Name
	}

	// TODO: consider adding cache
	operators, err := n.nodeStorage.ListOperators(0, 1000) // TODO: fix 1000
	if err != nil {
		n.logger.Warn("failed to get all operators for reporting", zap.Error(err))
	}

	allOperatorPubKeys := make([]string, 0)
	for _, operator := range operators {
		pubKeyHash := format.OperatorID(operator.PublicKey)
		allOperatorPubKeys = append(allOperatorPubKeys, pubKeyHash)
		if pubKeyHash == opPubKey {
			opIndex = strconv.FormatUint(operator.Index, 10)
			opName = operator.Name
		}
	}

	nodeState := n.idx.State(pid)
	n.logger.Info("peer identity",
		zap.String("peer", pid.String()),
		zap.String("forkv", forkv),
		zap.String("nodeVersion", nodeVersion),
		zap.String("opPubKey", opPubKey),
		zap.String("opIndex", opName),
		zap.String("opName", opIndex),
		zap.String("nodeType", nodeType),
		zap.String("nodeState", nodeState.String()),
		zap.Strings("allOpPubKeys", allOperatorPubKeys),
		zap.Bool("opFound", found),
		zap.NamedError("opDataErr", opDataErr),
		zap.Any("foundOpData", operatorData),
	)
	MetricsPeersIdentity.WithLabelValues(opPubKey, opIndex, opName, nodeVersion, pid.String(), nodeType).Set(1)
}

//
// func reportLastMsg(pid string) {
//	MetricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
// func timestamp() int64 {
//	return time.Now().UnixNano() / int64(time.Millisecond)
//}
