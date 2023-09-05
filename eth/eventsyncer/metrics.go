package eventsyncer

type metrics interface {
	LastBlockProcessed(uint64)
	LogsProcessingError(error)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (n nopMetrics) LastBlockProcessed(uint64) {}
func (n nopMetrics) LogsProcessingError(error) {}
