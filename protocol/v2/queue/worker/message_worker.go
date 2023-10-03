package worker

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

var (
	metricsMsgProcessing = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:worker:msg:process",
		Help: "Count decided messages",
	}, []string{"prefix"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricsMsgProcessing); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

// MsgHandler func that receive message.SSVMessage to handle
type MsgHandler func(msg *queue.DecodedSSVMessage) error

// ErrorHandler func that handles an error for a specific message
type ErrorHandler func(msg *queue.DecodedSSVMessage, err error) error

func defaultErrHandler(msg *queue.DecodedSSVMessage, err error) error {
	return err
}

// Config holds all necessary config for worker
type Config struct {
	Ctx          context.Context
	WorkersCount int
	Buffer       int
	MetrixPrefix string
}

// Worker listen to queue and process the messages
type Worker struct {
	ctx           context.Context
	cancel        context.CancelFunc
	workersCount  int
	queue         chan *queue.DecodedSSVMessage
	handler       MsgHandler
	errHandler    ErrorHandler
	metricsPrefix string
}

// NewWorker return new Worker
func NewWorker(logger *zap.Logger, cfg *Config) *Worker {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	w := &Worker{
		ctx:           ctx,
		cancel:        cancel,
		workersCount:  cfg.WorkersCount,
		queue:         make(chan *queue.DecodedSSVMessage, cfg.Buffer),
		errHandler:    defaultErrHandler,
		metricsPrefix: cfg.MetrixPrefix,
	}

	w.init(logger)

	return w
}

// init the worker listening process
func (w *Worker) init(logger *zap.Logger) {
	for i := 1; i <= w.workersCount; i++ {
		go w.startWorker(logger, w.queue)
	}
}

// startWorker process functionality
func (w *Worker) startWorker(logger *zap.Logger, ch <-chan *queue.DecodedSSVMessage) {
	ctx, cancel := context.WithCancel(w.ctx)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			w.process(logger, msg)
		}
	}
}

// UseHandler registers a message handler
func (w *Worker) UseHandler(handler MsgHandler) {
	w.handler = handler
}

// UseErrorHandler registers an error handler
func (w *Worker) UseErrorHandler(errHandler ErrorHandler) {
	w.errHandler = errHandler
}

// TryEnqueue tries to enqueue a job to the given job channel. Returns true if
// the operation was successful, and false if enqueuing would not have been
// possible without blocking. Job is not enqueued in the latter case.
func (w *Worker) TryEnqueue(msg *queue.DecodedSSVMessage) bool {
	select {
	case w.queue <- msg:
		return true
	default:
		return false
	}
}

// Close queue and worker listener
func (w *Worker) Close() {
	close(w.queue)
	w.cancel()
}

// Size returns the queue size
func (w *Worker) Size() int {
	return len(w.queue)
}

// process the msg's from queue
func (w *Worker) process(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	if w.handler == nil {
		logger.Warn("❗ no handler for worker")
		return
	}
	if err := w.handler(msg); err != nil {
		if handlerErr := w.errHandler(msg, err); handlerErr != nil {
			logger.Debug("❌ failed to handle message", zap.Error(handlerErr))
			return
		}
	}
	metricsMsgProcessing.WithLabelValues(w.metricsPrefix).Inc()
}
