package peers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
)

var (
	metricsSubnetsKnownPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:known",
		Help: "Counts known peers in subnets",
	}, []string{"subnet"})
	metricsSubnetsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:connected",
		Help: "Counts connected peers in subnets",
	}, []string{"subnet"})
	// metricsMySubnets marks subnets that this node is interested in
	metricsMySubnets = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:my",
		Help: "Marks subnets that this node is interested in",
	}, []string{"subnet"})
)

func init() {
	if err := prometheus.Register(metricsSubnetsKnownPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsSubnetsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsMySubnets); err != nil {
		log.Println("could not register prometheus collector")
	}
}
