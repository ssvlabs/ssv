package metricsreporter

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// TODO: implement all methods

type MetricsReporter struct {
}

func New() *MetricsReporter {
	return &MetricsReporter{}
}

func (m MetricsReporter) ExecutionClientReady() {

}

func (m MetricsReporter) ExecutionClientSyncing() {

}

func (m MetricsReporter) ExecutionClientFailure() {

}

func (m MetricsReporter) LastFetchedBlock(block uint64) {

}

func (m MetricsReporter) OperatorHasPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {

}

func (m MetricsReporter) ValidatorInactive(publicKey []byte) {

}

func (m MetricsReporter) ValidatorError(publicKey []byte) {

}

func (m MetricsReporter) ValidatorRemoved(publicKey []byte) {

}

func (m MetricsReporter) EventProcessed(eventName string) {

}

func (m MetricsReporter) EventProcessingFailed(eventName string) {

}
