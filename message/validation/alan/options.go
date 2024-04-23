package msgvalidation

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	operatordatastore "github.com/bloxapp/ssv/operator/datastore"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
)

// Option represents a functional option for configuring a messageValidator.
type Option func(validator *messageValidator)

// WithLogger sets the logger for the messageValidator.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// WithMetrics sets the metrics for the messageValidator.
func WithMetrics(metrics metricsreporter.MetricsReporter) Option {
	return func(mv *messageValidator) {
		mv.metrics = metrics
	}
}

// WithDutyStore sets the duty store for the messageValidator.
func WithDutyStore(dutyStore *dutystore.Store) Option {
	return func(mv *messageValidator) {
		mv.dutyStore = dutyStore
	}
}

// WithOwnOperatorID sets the operator ID getter for the messageValidator.
func WithOwnOperatorID(ods operatordatastore.OperatorDataStore) Option {
	return func(mv *messageValidator) {
		mv.operatorDataStore = ods
	}
}

// WithValidatorStore sets the validator store for the messageValidator.
func WithValidatorStore(validatorStore ValidatorStore) Option {
	return func(mv *messageValidator) {
		mv.validatorStore = validatorStore
	}
}

// WithSelfAccept blindly accepts messages sent from self. Useful for testing.
func WithSelfAccept(selfPID peer.ID, selfAccept bool) Option {
	return func(mv *messageValidator) {
		mv.selfPID = selfPID
		mv.selfAccept = selfAccept
	}
}
