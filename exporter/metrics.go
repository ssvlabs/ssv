package exporter

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	registrystorage "github.com/bloxapp/ssv/registry/storage"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	metricOperatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "index"})
)

func init() {
	if err := prometheus.Register(metricOperatorIndex); err != nil {
		zap.L().Debug("could not register prometheus collector", zap.Error(err))
	}
}

// ReportOperatorIndex reporting of new or exist operators
func ReportOperatorIndex(logger *zap.Logger, op *registrystorage.OperatorData) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(op.PublicKey))
	metricOperatorIndex.WithLabelValues(pkHash, strconv.FormatUint(op.ID, 10)).Set(float64(op.ID))
	logger.Debug("report operator", zap.String("pkHash", pkHash), zap.Uint64("id", op.ID))
}
