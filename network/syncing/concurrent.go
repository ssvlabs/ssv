package syncing

import (
	"context"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type Error struct {
	Operation string
	MessageID spectypes.MessageID
	Err       error
}

type concurrentSyncer struct {
	syncer Syncer
	ctx    context.Context
	jobs   chan func()
	errors chan<- Error
}

// NewConcurrent returns a new Syncer that runs the given Syncer's methods concurrently.
// concurrency is the number of worker goroutines to spawn.
// errors is a channel to which any errors are sent. Pass nil to discard errors.
func NewConcurrent(ctx context.Context, syncer Syncer, concurrency int, errors chan<- Error) Syncer {
	s := &concurrentSyncer{
		syncer: syncer,
		ctx:    ctx,
		jobs:   make(chan func(), concurrency),
		errors: errors,
	}

	// Spawn worker goroutines.
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range s.jobs {
				job()
			}
		}()
	}

	// Close the jobs channel when the context is done.
	go func() {
		<-ctx.Done()
		close(s.jobs)
	}()

	return s
}

func (s *concurrentSyncer) SyncHighestDecided(ctx context.Context, id spectypes.MessageID, handler MessageHandler) error {
	s.jobs <- func() {
		err := s.syncer.SyncHighestDecided(s.ctx, id, handler)
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

func (s *concurrentSyncer) SyncDecidedByRange(ctx context.Context, id spectypes.MessageID, from, to specqbft.Height, handler MessageHandler) error {
	s.jobs <- func() {
		err := s.syncer.SyncDecidedByRange(s.ctx, id, from, to, handler)
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
