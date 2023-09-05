package kv

import (
	"context"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// periodicallyCollectGarbage runs a QuickGC cycle periodically.
func (b *BadgerDB) periodicallyCollectGarbage(logger *zap.Logger, interval time.Duration) {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(interval):
			start := time.Now()
			err := b.QuickGC(context.Background())
			if err != nil {
				logger.Error("periodic GC cycle failed", zap.Error(err))
			} else {
				logger.Debug("periodic GC cycle completed", zap.Duration("took", time.Since(start)))
			}
		}
	}
}

// QuickGC runs a short garbage collection cycle to reclaim some unused disk space.
// Designed to be called periodically while the database is being used.
func (b *BadgerDB) QuickGC(ctx context.Context) error {
	b.gcMutex.Lock()
	defer b.gcMutex.Unlock()

	err := b.db.RunValueLogGC(0.5)
	if errors.Is(err, badger.ErrNoRewrite) {
		// No garbage to collect.
		return nil
	}
	return err
}

// FullGC runs a long garbage collection cycle to reclaim (ideally) all unused disk space.
// Designed to be called when the database is not being used.
func (b *BadgerDB) FullGC(ctx context.Context) error {
	b.gcMutex.Lock()
	defer b.gcMutex.Unlock()

	for ctx.Err() == nil {
		err := b.db.RunValueLogGC(0.1)
		if errors.Is(err, badger.ErrNoRewrite) {
			// No more garbage to collect.
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to collect garbage")
		}
	}
	return nil
}
