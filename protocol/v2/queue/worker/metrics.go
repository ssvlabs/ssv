package worker

type Metrics interface {
	WorkerProcessedMessage(prefix string)
}
