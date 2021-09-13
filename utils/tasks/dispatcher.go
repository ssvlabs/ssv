package tasks

import (
	"context"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	defaultConcurrentLimit = 100
)

var (
	// ErrTaskExist thrown when the task to dispatch already exist
	ErrTaskExist = errors.New("task exist")
)

type Fn func() error

// Task represents a some function to execute
type Task struct {
	Fn  Fn
	ID  string
	end func()
}

// NewTask creates a new task
func NewTask(fn Fn, id string, end func()) *Task {
	t := Task{fn, id, end}
	return &t
}

// Dispatcher maintains a queue of tasks to dispatch
type Dispatcher interface {
	// Queue adds a new task
	Queue(Task) error
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

	tasks   map[string]Task
	running map[string]bool
	waiting []string
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
		tasks:           map[string]Task{},
		waiting:         []string{},
		mut:             sync.RWMutex{},
		running:         map[string]bool{},
	}
	return &d
}

func (d *dispatcher) Queue(task Task) error {
	d.mut.Lock()
	defer d.mut.Unlock()

	if _, exist := d.tasks[task.ID]; exist {
		return ErrTaskExist
	}
	d.tasks[task.ID] = task
	d.waiting = append(d.waiting, task.ID)
	d.logger.Debug("task was queued",
		zap.String("task-id", task.ID),
		zap.Int("waiting", len(d.waiting)))
	return nil
}

func (d *dispatcher) nextTaskToRun() *Task {
	d.mut.Lock()
	defer d.mut.Unlock()

	if len(d.waiting) == 0 {
		return nil
	}
	// pop first task in the waiting queue
	tid := d.waiting[0]
	d.waiting = d.waiting[1:]
	if task, ok := d.tasks[tid]; ok {
		d.running[tid] = true
		return &task
	}
	return nil
}

func (d *dispatcher) Dispatch() {
	task := d.nextTaskToRun()
	if task == nil {
		return
	}
	go func() {
		tid := task.ID
		logger := d.logger.With(zap.String("task-id", tid))
		defer func() {
			d.mut.Lock()
			delete(d.running, tid)
			delete(d.tasks, tid)
			d.mut.Unlock()
		}()
		stats := d.Stats()
		logger.Debug("task was dispatched", zap.Time("time", stats.Time),
			zap.Int("running", stats.Running),
			zap.Int("waiting", stats.Waiting))
		err := task.Fn()
		if err != nil {
			logger.Error("task failed", zap.Error(err))
		}
		if task.end != nil {
			go task.end()
		}
	}()
}

func (d *dispatcher) Start() {
	if d.interval.Milliseconds() == 0 {
		d.logger.Warn("dispatcher interval was set to zero, ticker won't start")
		return
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.mut.RLock()
			running := len(d.running)
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
		Running: len(d.running),
		Time:    time.Now(),
	}
	return &ds
}
