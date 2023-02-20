package syncing

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"sync"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Error describes an error that occurred during a syncing operation.
type Error struct {
	Operation Operation
	Err       error
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

// Timeouts is a set of timeouts for each syncing operation.
type Timeouts struct {
	// SyncHighestDecided is the timeout for SyncHighestDecided.
	// Leave zero to not timeout.
	SyncHighestDecided time.Duration

	// SyncDecidedByRange is the timeout for SyncDecidedByRange.
	// Leave zero to not timeout.
	SyncDecidedByRange time.Duration
}

var DefaultTimeouts = Timeouts{
	SyncHighestDecided: 12 * time.Second,
	SyncDecidedByRange: 30 * time.Minute,
}

// Operation is a syncing operation that has been queued for execution.
type Operation interface {
	run(context.Context, *zap.Logger, Syncer) error
	timeout(Timeouts) time.Duration
}

type OperationSyncHighestDecided struct {
	ID      spectypes.MessageID
	Handler MessageHandler
}

func (o OperationSyncHighestDecided) run(ctx context.Context, logger *zap.Logger, s Syncer) error {
	return s.SyncHighestDecided(ctx, logger, o.ID, o.Handler)
}

func (o OperationSyncHighestDecided) timeout(t Timeouts) time.Duration {
	return t.SyncHighestDecided
}

func (o OperationSyncHighestDecided) String() string {
	return fmt.Sprintf("SyncHighestDecided(%s)", o.ID)
}

type OperationSyncDecidedByRange struct {
	ID      spectypes.MessageID
	From    specqbft.Height
	To      specqbft.Height
	Handler MessageHandler
}

func (o OperationSyncDecidedByRange) run(ctx context.Context, logger *zap.Logger, s Syncer) error {
	return s.SyncDecidedByRange(ctx, logger, o.ID, o.From, o.To, o.Handler)
}

func (o OperationSyncDecidedByRange) timeout(t Timeouts) time.Duration {
	return t.SyncDecidedByRange
}

func (o OperationSyncDecidedByRange) String() string {
	return fmt.Sprintf("SyncDecidedByRange(%s, %d, %d)", o.ID, o.From, o.To)
}

// ConcurrentSyncer is a Syncer that runs the given Syncer's methods concurrently.
type ConcurrentSyncer struct {
	syncer      Syncer
	ctx         context.Context
	jobs        chan Operation
	errors      chan<- Error
	concurrency int
	timeouts    Timeouts
}

// NewConcurrent returns a new Syncer that runs the given Syncer's methods concurrently.
// Unlike the standard syncer, syncing methods are non-blocking and return immediately without error.
// concurrency is the number of worker goroutines to spawn.
// errors is a channel to which any errors are sent. Pass nil to discard errors.
func NewConcurrent(
	ctx context.Context,
	syncer Syncer,
	concurrency int,
	timeouts Timeouts,
	errors chan<- Error,
) *ConcurrentSyncer {
	return &ConcurrentSyncer{
		syncer: syncer,
		ctx:    ctx,
		// TODO: make the buffer size configurable or better-yet unbounded?
		jobs:        make(chan Operation, 1024*1024),
		errors:      errors,
		concurrency: concurrency,
		timeouts:    timeouts,
	}
}

// Run starts the worker goroutines and blocks until the context is done
// and any remaining jobs are finished.
func (s *ConcurrentSyncer) Run(logger *zap.Logger) {
	// Spawn worker goroutines.
	var wg sync.WaitGroup
	for i := 0; i < s.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range s.jobs {
				s.do(logger, job)
			}
		}()
	}

	// Close the jobs channel when the context is done.
	<-s.ctx.Done()
	close(s.jobs)

	// Wait for workers to finish their current jobs.
	wg.Wait()
}

func (s *ConcurrentSyncer) do(logger *zap.Logger, job Operation) {
	ctx, cancel := context.WithTimeout(s.ctx, job.timeout(s.timeouts))
	defer cancel()
	err := job.run(ctx, logger, s.syncer)
	if err != nil && s.errors != nil {
		s.errors <- Error{
			Operation: job,
			Err:       err,
		}
	}
}

// Queued returns the number of jobs that are queued but not yet started.
func (s *ConcurrentSyncer) Queued() int {
	return len(s.jobs)
}

// Capacity returns the maximum number of jobs that can be queued.
// When Queued() == Capacity(), then the next call will block
// until a job is finished.
func (s *ConcurrentSyncer) Capacity() int {
	return cap(s.jobs)
}

func (s *ConcurrentSyncer) SyncHighestDecided(
	ctx context.Context,
	logger *zap.Logger,
	id spectypes.MessageID,
	handler MessageHandler,
) error {
	s.jobs <- OperationSyncHighestDecided{
		ID:      id,
		Handler: handler,
	}
	return nil
}

func (s *ConcurrentSyncer) SyncDecidedByRange(
	ctx context.Context,
	logger *zap.Logger,
	id spectypes.MessageID,
	from, to specqbft.Height,
	handler MessageHandler,
) error {
	s.jobs <- OperationSyncDecidedByRange{
		ID:      id,
		From:    from,
		To:      to,
		Handler: handler,
	}
	return nil
}
