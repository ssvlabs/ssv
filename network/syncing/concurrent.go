package syncing

import (
	"context"
	"fmt"
	"sync"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Error describes an error that occurred during a syncing operation.
type Error struct {
	Operation string
	MessageID spectypes.MessageID
	Err       error
}

func (e Error) Error() string {
	return fmt.Sprintf("%s(%s): %s", e.Operation, e.MessageID, e.Err)
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

// ConcurrentSyncer is a Syncer that runs the given Syncer's methods concurrently.
type ConcurrentSyncer struct {
	syncer      Syncer
	ctx         context.Context
	jobs        chan func()
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
		jobs:        make(chan func(), 1024*1024),
		errors:      errors,
		concurrency: concurrency,
		timeouts:    timeouts,
	}
}

// Run starts the worker goroutines and blocks until the context is done
// and any remaining jobs are finished.
func (s *ConcurrentSyncer) Run() {
	// Spawn worker goroutines.
	var wg sync.WaitGroup
	for i := 0; i < s.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range s.jobs {
				job()
			}
		}()
	}

	// Close the jobs channel when the context is done.
	<-s.ctx.Done()
	close(s.jobs)

	// Wait for workers to finish their current jobs.
	wg.Wait()
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
	id spectypes.MessageID,
	handler MessageHandler,
) error {
	s.jobs <- func() {
		err := s.syncer.SyncHighestDecided(ctx, id, handler)
		if err != nil && s.errors != nil {
			s.errors <- Error{
				Operation: "SyncHighestDecided",
				MessageID: id,
				Err:       err,
			}
		}
	}
	return nil
}

func (s *ConcurrentSyncer) SyncDecidedByRange(
	ctx context.Context,
	id spectypes.MessageID,
	from, to specqbft.Height,
	handler MessageHandler,
) error {
	s.jobs <- func() {
		err := s.syncer.SyncDecidedByRange(ctx, id, from, to, handler)
		if err != nil && s.errors != nil {
			s.errors <- Error{
				Operation: "SyncDecidedByRange",
				MessageID: id,
				Err:       err,
			}
		}
	}
	return nil
}
