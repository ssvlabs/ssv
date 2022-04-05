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
	cancel       context.CancelFunc
	logger       *zap.Logger
	workersCount int
	queue        chan *message.SSVMessage
	handler      workerHandler
}

func NewWorker(cfg *WorkerConfig) *Worker {
	ctx, cancel := context.WithCancel(cfg.Ctx)
	logger := cfg.Logger.With(zap.String("who", "messageWorker"))

	w := &Worker{
		ctx:          ctx,
		logger:       logger,
		cancel:       cancel,
		workersCount: cfg.WorkersCount,
		queue:        make(chan *message.SSVMessage, cfg.Buffer),
	}

	w.init()

	return w
}

func (w *Worker) init() {
	for i := 1; i <= w.workersCount; i++ {
		go w.startWorker(w.queue)
	}
}

func (w *Worker) startWorker(ch <-chan *message.SSVMessage) {
	for {
		select {
		case <-w.ctx.Done():
			return

		case msg := <-ch:
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
