package worker

import (
	"context"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"go.uber.org/zap"
)

type workerHandler func(msg *message.SSVMessage)

type WorkerConfig struct {
	Ctx          context.Context
	Logger       *zap.Logger
	WorkersCount int
	Buffer       int
}

type Worker struct {
	ctx          context.Context
	logger       *zap.Logger
	cancel       context.CancelFunc
	workersCount int
	queue        chan *message.SSVMessage
	handler      workerHandler
}

func NewWorker(cfg *WorkerConfig) *Worker {
	ctx, cancel := context.WithCancel(cfg.Ctx)
	logger := cfg.Logger.With(zap.String("who", "messageWorker"))

	return &Worker{
		ctx:          ctx,
		logger:       logger,
		cancel:       cancel,
		workersCount: cfg.WorkersCount,
		queue:        make(chan *message.SSVMessage, cfg.Buffer),
	}
}

// Init listen to queue and process each new msg
func (w *Worker) Init() {
	for {
		select {
		case <-w.ctx.Done():
			return

		case msg := <-w.queue:
			w.process(msg)
		}
	}
}

func (w *Worker) AddHandler(handler workerHandler) {
	w.handler = handler
}

// TryEnqueue tries to enqueue a job to the given job channel. Returns true if
// the operation was successful, and false if enqueuing would not have been
// possible without blocking. Job is not enqueued in the latter case.
func (w *Worker) TryEnqueue(msg *message.SSVMessage) bool {
	select {
	case w.queue <- msg:
		return true
	default:
		return false
	}
}

func (w *Worker) Close() {
	close(w.queue)
	w.cancel()
}

func (w *Worker) process(msg *message.SSVMessage) {
	if w.handler == nil {
		w.logger.Warn("no handler for worker")
	}
	w.handler(msg)
}
