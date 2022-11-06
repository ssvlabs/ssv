package instance

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricsIBFTStage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_stage",
		Help: "IBFTs stage",
	}, []string{"identifier", "pubKey"})
	metricsIBFTRound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_round",
		Help: "IBFTs round",
	}, []string{"identifier", "pubKey"})
	metricsStageTimeProposal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:stage_time_proposal",
		Help: "Proposal stage time (seconds)",
	}, []string{"identifier", "pubKey"})
	metricsStageTimePrepare = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:proposal_stage_time",
		Help: "Prepare stage time (seconds)",
	}, []string{"identifier", "pubKey"})
	metricsStageTimeCommit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:stage_time_commit",
		Help: "Commit stage time (seconds)",
	}, []string{"identifier", "pubKey"})
)

func init() {
	if err := prometheus.Register(metricsIBFTStage); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsIBFTRound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsStageTimeProposal); err != nil {
		log.Println("could not register prometheus collector")
	}
}
