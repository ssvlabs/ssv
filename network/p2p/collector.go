package p2p

import (
	"fmt"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"sort"
)

const (
	collectorID = "network"
	// metrics:
	peersCount             = "peers_count"
	internalListenersCount = "internal_listeners_count"
	pubsubTopicsCount = "pubsub_topics_count"
)

// SetupNetworkMetrics initialize collector for process metrics
func SetupNetworkMetrics(logger *zap.Logger, net network.Network) {
	c := networkCollector{logger, net.(*p2pNetwork)}
	metrics.Register(&c)
}

// networkCollector implements metrics.Collector for validators information
type networkCollector struct {
	logger *zap.Logger
	network *p2pNetwork
}

func (c *networkCollector) ID() string {
	return collectorID
}

func (c *networkCollector) Collect() ([]string, error) {
	var results []string

	//c.network.listenersLock.Lock()
	results = append(results, fmt.Sprintf("%s{} %d", internalListenersCount,
		len(c.network.listeners)))
	//c.network.listenersLock.Unlock()

	//c.logger.Debug("collecting", zap.String("metric", peersCount))
	//results = append(results, fmt.Sprintf("%s{} %d", peersCount,
	//	len(c.network.peers.Active())))

	results = append(results, fmt.Sprintf("%s{} %d", pubsubTopicsCount,
		len(c.network.pubsub.GetTopics())))

	sort.Strings(results)

	return results, nil
}

