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
	metricsDurationStageProposal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:duration_stage_proposal",
		Help: "Proposal stage duration (seconds)",
	}, []string{"identifier", "pubKey"})
	metricsDurationStagePrepare = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:duration_stage_prepare",
		Help: "Prepare stage duration (seconds)",
	}, []string{"identifier", "pubKey"})
	metricsDurationStageTimeCommit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:duration_stage_commit",
		Help: "Commit stage duration (seconds)",
	}, []string{"identifier", "pubKey"})
)

func init() {
	if err := prometheus.Register(metricsIBFTStage); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsIBFTRound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsDurationStageProposal); err != nil {
		log.Println("could not register prometheus collector")
	}
}
