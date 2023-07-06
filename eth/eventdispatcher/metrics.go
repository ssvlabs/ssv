package eventdispatcher

// TODO: define
type metrics interface {
	HistoricalLogsProcessed(uint64)
	LastBlockProcessed(uint64)
	LogsProcessingError(error)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (n nopMetrics) LastBlockProcessed(uint64)      {}
func (n nopMetrics) HistoricalLogsProcessed(uint64) {}
func (n nopMetrics) LogsProcessingError(error)      {}
