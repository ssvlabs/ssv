package forks

import (
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

func TestForker_StartBeforeFork(t *testing.T) {
	done := false
	forker := NewForker(Config{
		Network:    "prater",
		Logger:     logex.GetLogger(),
		BeforeFork: dummyV0Fork{},
		PostFork:   dummyV1Fork{},
		ForkSlot:   1,
	})

	forker.AddHandler(func(slot uint64) {
		done = true
	})
	require.False(t, forker.IsForked())
	_, ok := forker.GetCurrentFork().(dummyV0Fork)
	require.True(t, ok)
	forker.Start()
	require.True(t, forker.IsForked())
	time.Sleep(time.Millisecond * 100)
	require.True(t, done)
	_, ok = forker.GetCurrentFork().(dummyV1Fork)
	require.True(t, ok)
}

func TestForker_StartPostFork(t *testing.T) {
	forker := getForker(100000000)

	forker.Start()
	require.False(t, forker.IsForked())
}

func getForker(forkSlot uint64) *Forker {
	return NewForker(Config{
		Network:    "prater",
		Logger:     logex.GetLogger(),
		BeforeFork: dummyV0Fork{},
		PostFork:   dummyV0Fork{},
		ForkSlot:   forkSlot,
	})
}

type dummyV0Fork struct {
	Fork
}

type dummyV1Fork struct {
	Fork
}
