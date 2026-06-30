package worker

import (
	"context"
	"sync"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// MsgHandler func that receive message.SSVMessage to handle
type MsgHandler func(ctx context.Context, msg network.DecodedSSVMessage) error

// ErrorHandler func that handles an error for a specific message
type ErrorHandler func(msg *queue.SSVMessage, err error) error

func defaultErrHandler(msg *queue.SSVMessage, err error) error {
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
	queue         chan *queue.SSVMessage
	handler       MsgHandler
	errHandler    ErrorHandler
	metricsPrefix string
	// wg tracks the startWorker goroutines so Close can wait for any in-flight
	// process() to finish. This lets callers quiesce the worker before closing
	// resources (e.g. the DB) that a handler might still write to.
	wg sync.WaitGroup
}

// NewWorker return new Worker
func NewWorker(logger *zap.Logger, cfg *Config) *Worker {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	w := &Worker{
		ctx:           ctx,
		cancel:        cancel,
		workersCount:  cfg.WorkersCount,
		queue:         make(chan *queue.SSVMessage, cfg.Buffer),
		errHandler:    defaultErrHandler,
		metricsPrefix: cfg.MetrixPrefix,
	}

	w.init(logger)

	return w
}

// init the worker listening process
func (w *Worker) init(logger *zap.Logger) {
	for i := 1; i <= w.workersCount; i++ {
		w.wg.Add(1)
		go w.startWorker(logger, w.queue)
	}
}

// startWorker process functionality
func (w *Worker) startWorker(logger *zap.Logger, ch <-chan *queue.SSVMessage) {
	defer w.wg.Done()
	ctx, cancel := context.WithCancel(w.ctx)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			w.process(ctx, logger, msg)
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
func (w *Worker) TryEnqueue(msg *queue.SSVMessage) bool {
	select {
	case w.queue <- msg:
		return true
	default:
		return false
	}
}

// Close stops the worker goroutines and waits for any in-flight message
// processing to finish. Callers can rely on no handler being mid-execution
// once Close returns — important when a handler writes to resources (e.g. the
// DB) that are torn down right after. The queue is intentionally left open:
// startWorker exits on ctx cancellation, and a closed queue would let it read
// a nil message and feed it to the handler.
func (w *Worker) Close() {
	w.cancel()
	w.wg.Wait()
}

// Size returns the queue size
func (w *Worker) Size() int {
	return len(w.queue)
}

func messageContextFields(msg *queue.SSVMessage) []zap.Field {
	if msg == nil {
		return nil
	}

	role := msg.MsgID.GetRoleType()
	logFields := []zap.Field{
		fields.MessageID(msg.MsgID),
		fields.MessageType(msg.MsgType),
		fields.RunnerRole(role),
	}

	if slot, err := msg.Slot(); err == nil {
		logFields = append(logFields, fields.Slot(slot))
	}

	if role == spectypes.RoleCommittee {
		executorID := msg.MsgID.GetDutyExecutorID()
		if len(executorID) >= 32 {
			var committeeID spectypes.CommitteeID
			// committeeID is the last 16 bytes of the executorID
			copy(committeeID[:], executorID[16:])
			logFields = append(logFields, fields.CommitteeID(committeeID))
		}
	}

	return logFields
}

// process the msg's from queue
func (w *Worker) process(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) {
	if w.handler == nil {
		logger.Warn("❗ no handler for worker")
		return
	}
	if err := w.handler(ctx, msg); err != nil {
		if handlerErr := w.errHandler(msg, err); handlerErr != nil {
			logger.Debug("❌ failed to handle message", append(messageContextFields(msg), zap.Error(handlerErr))...)
			return
		}
	}
}
