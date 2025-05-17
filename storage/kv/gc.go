package kv

import (
	"context"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// periodicallyCollectGarbage runs a QuickGC cycle periodically.
func (b *BadgerDB) periodicallyCollectGarbage(interval time.Duration) {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(interval):
			start := time.Now()
			err := b.QuickGC(context.Background())
			if err != nil {
				b.logger.Error("periodic GC cycle failed", zap.Error(err))
			} else {
				b.logger.Debug("periodic GC cycle completed", zap.Duration("took", time.Since(start)))
			}
		}
	}
}

// QuickGC runs a short garbage collection cycle to reclaim some unused disk space.
// Designed to be called periodically while the database is being used.
func (b *BadgerDB) QuickGC(ctx context.Context) error {
	return b.gc(ctx, 0.7)
}

// FullGC runs a long garbage collection cycle to reclaim (ideally) all unused disk space.
// Designed to be called when the database is not being used.
func (b *BadgerDB) FullGC(ctx context.Context) error {
	return b.gc(ctx, 0.1)
}

func (b *BadgerDB) gc(ctx context.Context, discardRatio float64) error {
	b.gcMutex.Lock()
	defer b.gcMutex.Unlock()

	for ctx.Err() == nil {
		err := b.db.RunValueLogGC(discardRatio)
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
