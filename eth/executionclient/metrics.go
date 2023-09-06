package executionclient

type metrics interface {
	ExecutionClientReady()
	ExecutionClientSyncing()
	ExecutionClientFailure()
	ExecutionClientLastFetchedBlock(block uint64)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (nopMetrics) ExecutionClientReady()                    {}
func (nopMetrics) ExecutionClientSyncing()                  {}
func (nopMetrics) ExecutionClientFailure()                  {}
func (nopMetrics) ExecutionClientLastFetchedBlock(_ uint64) {}
