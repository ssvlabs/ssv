package types

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	MetricsSignaturesVerifications = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_signature_verifications",
		Help: "Number of signatures verifications",
	}, []string{})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(MetricsSignaturesVerifications); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

func VerifyReconstructedSignature(sig *bls.Sign, validatorPubKey []byte, root [32]byte) error {
	MetricsSignaturesVerifications.WithLabelValues().Inc()

	pk, err := DeserializeBLSPublicKey(validatorPubKey)
	if err != nil {
		return errors.Wrap(err, "could not deserialize validator pk")
	}

	if res := sig.VerifyByte(&pk, root[:]); !res {
		return errors.New("could not reconstruct a valid signature")
	}
	return nil
}
