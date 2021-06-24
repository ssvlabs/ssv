package tasks

import (
	"context"
	"go.uber.org/zap"
	"sync"
	"time"
)

var (
	defaultConcurrentLimit = 10
)

// Task represents a some function to execute
type Task struct {
	Fn func() error
	ID string
}

// NewTask creates a new task
func NewTask(fn func() error, id string) *Task {
	t := Task{fn, id}
	return &t
}

// Dispatcher maintains a queue of tasks to dispatch
type Dispatcher interface {
	// Queue adds a new task
	Queue(Task)
	// Dispatch will dispatch the next task
	Dispatch()
	// Start starts ticks
	Start()
	// Stats returns the number of waiting tasks and the number of running tasks
	Stats() *DispatcherStats
}

// DispatcherOptions describes the needed arguments for dispatcher instance
type DispatcherOptions struct {
	// Ctx is a context for stopping the dispatcher
	Ctx context.Context
	// Logger used for logs
	Logger *zap.Logger
	// Interval is the time interval ticker used by dispatcher
	// if the value was not provided (zero) -> no interval will run.
	// *the calls to Dispatch() should be in a higher level
	Interval time.Duration
	// Concurrent is the limit of concurrent tasks running
	// if zero or negative (<= 0) then defaultConcurrentLimit will be used
	Concurrent int
}

// DispatcherStats represents runtime stats of the dispatcher
type DispatcherStats struct {
	// Waiting is the number of tasks that waits in queue
	Waiting int
	// Running is the number of running tasks
	Running int
	// Time is the time when the stats snapshot was taken
	Time time.Time
}

// dispatcher is the internal implementation of Dispatcher
type dispatcher struct {
	ctx    context.Context
	logger *zap.Logger

	running int
	waiting []Task
	mut     sync.RWMutex

	interval        time.Duration
	concurrentLimit int
}

// NewDispatcher creates a new instance
func NewDispatcher(opts DispatcherOptions) Dispatcher {
	if opts.Concurrent == 0 {
		opts.Concurrent = defaultConcurrentLimit
	}
	d := dispatcher{
		ctx:             opts.Ctx,
		logger:          opts.Logger,
		interval:        opts.Interval,
		concurrentLimit: opts.Concurrent,
		waiting:         []Task{},
		mut:             sync.RWMutex{},
		running:         0,
	}
	return &d
}

func (d *dispatcher) Queue(task Task) {
	d.mut.Lock()
	defer d.mut.Unlock()

	d.waiting = append(d.waiting, task)
	d.logger.Debug("task was queued",
		zap.String("task-id", task.ID),
		zap.Int("waitingTasks", len(d.waiting)))
}

func (d *dispatcher) nextTaskToRun() *Task {
	d.mut.Lock()
	defer d.mut.Unlock()

	if len(d.waiting) == 0 {
		return nil
	}
	// pop first task in the waiting queue
	task := d.waiting[0]
	d.waiting = d.waiting[1:]
	d.running++
	return &task
}

func (d *dispatcher) Dispatch() {
	task := d.nextTaskToRun()
	if task == nil {
		return
	}
	go func() {
		defer func() {
			d.mut.Lock()
			d.running--
			d.mut.Unlock()
		}()
		d.logger.Debug("task was dispatched", zap.String("task-id", task.ID))
		err := task.Fn()
		if err != nil {
			d.logger.Error("task failed", zap.Error(err),
				zap.String("task-id", task.ID))
		}
	}()
}

func (d *dispatcher) Start() {
	if d.interval.Milliseconds() == 0 {
		d.logger.Debug("dispatcher interval was set to zero, ticker won't start")
		return
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.mut.RLock()
			running := d.running
			d.mut.RUnlock()
			if running < d.concurrentLimit {
				d.Dispatch()
			}
		case <-d.ctx.Done():
			d.logger.Debug("Context closed, exiting dispatcher interval routine")
			return
		}
	}
}

func (d *dispatcher) Stats() *DispatcherStats {
	d.mut.RLock()
	defer d.mut.RUnlock()
	ds := DispatcherStats{
		Waiting: len(d.waiting),
		Running: d.running,
		Time:    time.Now(),
	}
	return &ds
}
