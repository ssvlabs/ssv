package versionprobe

import (
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func pid(i int) peer.ID { return peer.ID(fmt.Sprintf("peer-%04d", i)) }

func TestCacheDueWhenUnknown(t *testing.T) {
	c := NewCache(10)
	require.True(t, c.Due(pid(1), t0))
}

func TestCacheOKRefreshesAfterAnHour(t *testing.T) {
	c := NewCache(10)
	e := c.Record(pid(1), Result{Outcome: OutcomeOK, Version: "v2.5.0"}, t0)
	require.Equal(t, t0.Add(time.Hour), e.NextAttempt)
	require.Equal(t, t0, e.ProbedAt)
	require.False(t, c.Due(pid(1), t0.Add(59*time.Minute)))
	require.True(t, c.Due(pid(1), t0.Add(time.Hour)))
	v, at, ok := c.Version(pid(1))
	require.True(t, ok)
	require.Equal(t, "v2.5.0", v)
	require.Equal(t, t0, at)
}

func TestCacheBackoffSchedule(t *testing.T) {
	c := NewCache(10)
	want := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 2 * time.Hour}
	now := t0
	for i, w := range want {
		e := c.Record(pid(1), Result{Outcome: OutcomeTimeout, Err: "deadline"}, now)
		require.Equal(t, now.Add(w), e.NextAttempt, "failure %d", i+1)
		require.Equal(t, i+1, e.Attempts)
		now = e.NextAttempt
	}
}

func TestCacheSuccessResetsBackoffAndFailureKeepsVersion(t *testing.T) {
	c := NewCache(10)
	c.Record(pid(1), Result{Outcome: OutcomeRefused}, t0)
	c.Record(pid(1), Result{Outcome: OutcomeOK, Version: "v2.5.0"}, t0.Add(time.Hour))
	e := c.Record(pid(1), Result{Outcome: OutcomeTimeout}, t0.Add(3*time.Hour))
	require.Equal(t, t0.Add(3*time.Hour).Add(15*time.Minute), e.NextAttempt) // first failure again
	require.Equal(t, "v2.5.0", e.Version)
	require.Equal(t, t0.Add(time.Hour), e.ProbedAt)
	require.Equal(t, OutcomeTimeout, e.Outcome)
	v, _, ok := c.Version(pid(1))
	require.True(t, ok)
	require.Equal(t, "v2.5.0", v)
}

func TestCacheVersionUnknownWithoutSuccess(t *testing.T) {
	c := NewCache(10)
	c.Record(pid(1), Result{Outcome: OutcomeNoAddr}, t0)
	_, _, ok := c.Version(pid(1))
	require.False(t, ok)
}

func TestCacheEvictsOldestLastAttempt(t *testing.T) {
	c := NewCache(3)
	c.Record(pid(1), Result{Outcome: OutcomeOK, Version: "a"}, t0.Add(2*time.Minute))
	c.Record(pid(2), Result{Outcome: OutcomeOK, Version: "b"}, t0) // oldest
	c.Record(pid(3), Result{Outcome: OutcomeOK, Version: "c"}, t0.Add(time.Minute))
	c.Record(pid(4), Result{Outcome: OutcomeOK, Version: "d"}, t0.Add(3*time.Minute))
	_, _, ok := c.Version(pid(2))
	require.False(t, ok)
	require.Len(t, c.Entries(), 3)
}

func TestCacheEntriesNewestFirst(t *testing.T) {
	c := NewCache(10)
	c.Record(pid(1), Result{Outcome: OutcomeOK}, t0)
	c.Record(pid(2), Result{Outcome: OutcomeOK}, t0.Add(time.Minute))
	es := c.Entries()
	require.Equal(t, pid(2), es[0].PeerID)
	require.Equal(t, pid(1), es[1].PeerID)
}
