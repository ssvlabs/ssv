package exporter

import (
	"fmt"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	metricOperatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "name"})
)

func init() {
	if err := prometheus.Register(metricOperatorIndex); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// TODO: (un-lint)
//nolint
func reportOperatorIndex(logger *zap.Logger, op *registrystorage.OperatorInformation) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256([]byte(op.PublicKey)))
	metricOperatorIndex.WithLabelValues(pkHash, op.Name).Set(float64(op.Index))
	logger.Debug("report operator", zap.String("pkHash", pkHash),
		zap.String("name", op.Name), zap.Int64("index", op.Index))
}
