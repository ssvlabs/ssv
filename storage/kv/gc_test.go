package kv

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// TestQuickGC_EmptyDB verifies that QuickGC succeeds on an empty database.
func TestQuickGC_EmptyDB(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-quickgc-empty")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.QuickGC(context.Background()))
}

// TestQuickGC_WithGarbage verifies that QuickGC reclaims garbage after data deletion.
func TestQuickGC_WithGarbage(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-quickgc-garbage")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	prefix := []byte("quickgc")

	setupDataset(t, db, prefix, 100)

	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("test-%d", i))

		require.NoError(t, db.Delete(prefix, key))
	}

	require.NoError(t, db.QuickGC(context.Background()))
}

// TestQuickGC_ErrorWhenClosed verifies that QuickGC returns an error when the database is closed.
func TestQuickGC_ErrorWhenClosed(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-quickgc-error")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.Error(t, db.QuickGC(context.Background()))
}

// TestFullGC_EmptyDB verifies that FullGC succeeds on an empty database.
func TestFullGC_EmptyDB(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-fullgc-empty")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.FullGC(context.Background()))
}

// TestFullGC_WithGarbage verifies that FullGC reclaims garbage after data deletion.
func TestFullGC_WithGarbage(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-fullgc-garbage")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	prefix := []byte("fullgc")

	setupDataset(t, db, prefix, 100)

	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("test-%d", i))

		require.NoError(t, db.Delete(prefix, key))
	}

	require.NoError(t, db.FullGC(context.Background()))
}

// TestFullGC_ErrorWhenClosed verifies that FullGC returns a wrapped error when the database is closed.
func TestFullGC_ErrorWhenClosed(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-fullgc-error")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = db.FullGC(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to collect garbage")
}

// TestFullGC_ContextCancellation verifies that FullGC exits promptly when the context is cancelled.
func TestFullGC_ContextCancellation(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-fullgc-ctx")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, db.FullGC(ctx))
}

// TestGCLock_MutualExclusion verifies that GC operations are mutually exclusive.
func TestGCLock_MutualExclusion(t *testing.T) {
	t.Parallel()

	dir := setupTempDir(t, "badger-gc-lock")
	logger := logging.TestLogger(t)
	db, err := New(logger, basedb.Options{Path: dir})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	lockAcquired := make(chan struct{})
	tryLock := make(chan struct{})
	releaseLock := make(chan struct{})
	lockAcquired2 := make(chan struct{})

	go func() {
		db.gcMutex.Lock()
		close(lockAcquired)
		<-releaseLock
		db.gcMutex.Unlock()
	}()
	<-lockAcquired

	go func() {
		<-tryLock
		db.gcMutex.Lock()
		close(lockAcquired2)
		db.gcMutex.Unlock()
	}()
	close(tryLock)

	time.Sleep(50 * time.Millisecond)
	select {
	case <-lockAcquired2:
		t.Fatal("second goroutine acquired lock prematurely")
	default:
	}

	close(releaseLock)
	select {
	case <-lockAcquired2:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second goroutine did not acquire lock after release")
	}
}

// TestPeriodicGC verifies that periodic GC cycles are executed and logged correctly.
func TestPeriodicGC(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	dir := setupTempDir(t, "badger-periodic-gc")
	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	db, err := New(logger, basedb.Options{
		Path: dir,
		Ctx:  ctx,
	})

	require.NoError(t, err)

	t.Cleanup(func() {
		cancel()
		require.NoError(t, db.Close())
	})

	db.wg.Add(1)
	go db.periodicallyCollectGarbage(logger, 20*time.Millisecond)

	prefix := []byte("periodic")

	setupDataset(t, db, prefix, 100)

	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("test-%d", i))

		require.NoError(t, db.Delete(prefix, key))
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	db.wg.Wait()

	completed := logs.FilterMessage("periodic GC cycle completed").All()

	assert.Greater(t, len(completed), 0)

	for _, entry := range completed {
		found := false
		for _, field := range entry.Context {
			if field.Key == "took" {
				found = true
				break
			}
		}
		assert.True(t, found)
	}
}

// TestPeriodicGC_Cancellation verifies that the periodic GC goroutine exits promptly when the context is cancelled.
func TestPeriodicGC_Cancellation(t *testing.T) {
	t.Parallel()

	logger := logging.TestLogger(t)
	dir := setupTempDir(t, "badger-periodic-gc-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	db, err := New(logger, basedb.Options{
		Path: dir,
		Ctx:  ctx,
	})

	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	db.wg.Add(1)
	go db.periodicallyCollectGarbage(logger, 10*time.Hour)

	cancel()
	done := make(chan struct{})
	go func() {
		db.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("periodic GC did not cancel promptly")
	}
}
