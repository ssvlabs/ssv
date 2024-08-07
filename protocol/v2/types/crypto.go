package types

import (
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
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

func ReconstructSignature(ps *specssv.PartialSigContainer, root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	// Reconstruct signatures
	if ps.Signatures[validatorIndex] == nil {
		return nil, errors.New("no signatures for the given validator index")
	}
	if ps.Signatures[validatorIndex][signingRootHex(root)] == nil {
		return nil, errors.New("no signatures for the given signing root")
	}

	operatorsSignatures := make(map[uint64][]byte)
	for operatorID, sig := range ps.Signatures[validatorIndex][signingRootHex(root)] {
		operatorsSignatures[operatorID] = sig
	}
	signature, err := spectypes.ReconstructSignatures(operatorsSignatures)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reconstruct signatures")
	}

	// Get validator pub key copy (This avoids cgo Go pointer to Go pointer issue)
	validatorPubKeyCopy := make([]byte, len(validatorPubKey))
	copy(validatorPubKeyCopy, validatorPubKey)

	if err := VerifyReconstructedSignature(signature, validatorPubKeyCopy, root); err != nil {
		return nil, errors.Wrap(err, "failed to verify reconstruct signature")
	}
	return signature.Serialize(), nil
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

func signingRootHex(r [32]byte) specssv.SigningRoot {
	return specssv.SigningRoot(hex.EncodeToString(r[:]))
}
