package syncing_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/syncing"
	"github.com/bloxapp/ssv/network/syncing/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSyncer(t *testing.T) {
	logger := logex.TestLogger(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Test setup
	syncer := mocks.NewMockSyncer(ctrl)
	errors := make(chan syncing.Error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	concurrency := 2
	s := syncing.NewConcurrent(ctx, syncer, concurrency, syncing.DefaultTimeouts, errors)

	// Run the syncer
	done := make(chan struct{})
	go func() {
		s.Run(logger)
		close(done)
	}()

	// Test SyncHighestDecided
	id := spectypes.MessageID{}
	handler := newMockMessageHandler()
	syncer.EXPECT().SyncHighestDecided(gomock.Any(), gomock.Any(), id, gomock.Any()).Return(nil)
	s.SyncHighestDecided(ctx, logger, id, handler.handler)

	// Test SyncDecidedByRange
	from := specqbft.Height(1)
	to := specqbft.Height(10)
	syncer.EXPECT().SyncDecidedByRange(gomock.Any(), gomock.Any(), id, from, to, gomock.Any()).Return(nil)
	s.SyncDecidedByRange(ctx, logger, id, from, to, handler.handler)

	// Test error handling
	syncer.EXPECT().SyncHighestDecided(gomock.Any(), gomock.Any(), id, gomock.Any()).Return(fmt.Errorf("test error"))
	s.SyncHighestDecided(ctx, logger, id, handler.handler)

	// Wait for the syncer to finish
	cancel()

	// Verify errors.
	select {
	case err := <-errors:
		require.IsType(t, syncing.OperationSyncHighestDecided{}, err.Operation)
		require.Equal(t, id, err.Operation.(syncing.OperationSyncHighestDecided).ID)
		require.Equal(t, "test error", err.Err.Error())
	case <-done:
		t.Fatal("error channel should have received an error")
	}
	<-done
}

func TestConcurrentSyncerMemoryUsage(t *testing.T) {
	logger := logex.TestLogger(t)

	for i := 0; i < 4; i++ {
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		// Test setup
		syncer := &mockSyncer{}
		errors := make(chan syncing.Error)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		concurrency := 2
		s := syncing.NewConcurrent(ctx, syncer, concurrency, syncing.DefaultTimeouts, errors)

		// Run the syncer
		done := make(chan struct{})
		go func() {
			s.Run(logger)
			close(done)
		}()

		for i := 0; i < 1024*128; i++ {
			// Test SyncHighestDecided
			id := spectypes.MessageID{}
			handler := newMockMessageHandler()
			s.SyncHighestDecided(ctx, logger, id, handler.handler)

			// Test SyncDecidedByRange
			from := specqbft.Height(1)
			to := specqbft.Height(10)
			s.SyncDecidedByRange(ctx, logger, id, from, to, handler.handler)
		}

		// Wait for the syncer to finish
		cancel()
		<-done

		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		t.Logf("Allocated: %.2f MB", float64(after.TotalAlloc-before.TotalAlloc)/1024/1024)
	}
}

func BenchmarkConcurrentSyncer(b *testing.B) {
	logger := logex.Build(b.Name(), zap.DebugLevel, nil)

	for i := 0; i < b.N; i++ {
		// Test setup
		syncer := &mockSyncer{}
		errors := make(chan syncing.Error)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		concurrency := 2
		s := syncing.NewConcurrent(ctx, syncer, concurrency, syncing.DefaultTimeouts, errors)

		// Run the syncer
		done := make(chan struct{})
		go func() {
			s.Run(logger)
			close(done)
		}()

		for i := 0; i < 1024*128; i++ {
			// Test SyncHighestDecided
			id := spectypes.MessageID{}
			handler := newMockMessageHandler()
			s.SyncHighestDecided(ctx, logger, id, handler.handler)

			// Test SyncDecidedByRange
			from := specqbft.Height(1)
			to := specqbft.Height(10)
			s.SyncDecidedByRange(ctx, logger, id, from, to, handler.handler)
		}

		// Wait for the syncer to finish
		cancel()
		<-done
	}
}
