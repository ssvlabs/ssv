package faults

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// specMenu is the fault menu exactly as docs/qa-glamsterdam-test-plan-passes.md section 6.3 lists
// it. The test below pins the code to the document: a value added on one side and not the other is
// a test failure, not a silent drift.
var specMenu = []string{
	"vote-112b", "vote-index-2", "vote-index-flip", "double-vote-index",
	"ptc-qbft", "block-wrong-version",
	"envelope-foreign-root", "envelope-builder-index",
	"prefs-5-roots", "prefs-conflict", "prefs-early", "prefs-late", "prefs-34-apart", "prefs-replay",
	"ptc-3-per-epoch", "role7-prefork", "vr-postfork", "two-entries", "auth-no-builders",
}

func TestMenuMatchesSpec(t *testing.T) {
	require.ElementsMatch(t, specMenu, Names())
	require.Len(t, Names(), 19)
}

func TestEveryEntryIsDocumented(t *testing.T) {
	for _, e := range Menu() {
		require.NotEmpty(t, e.Scenarios, "fault %q has no scenario IDs", e.Fault)
		require.NotEmpty(t, e.Behaviour, "fault %q has no behaviour text", e.Fault)
		require.NotEmpty(t, e.Site, "fault %q has no injection site", e.Fault)
	}
}

func TestParse(t *testing.T) {
	t.Run("empty is none", func(t *testing.T) {
		got, err := Parse("")
		require.NoError(t, err)
		require.Equal(t, None, got)
	})

	t.Run("none is none", func(t *testing.T) {
		got, err := Parse("none")
		require.NoError(t, err)
		require.Equal(t, None, got)
	})

	t.Run("whitespace and case are tolerated", func(t *testing.T) {
		got, err := Parse("  Vote-112b ")
		require.NoError(t, err)
		require.Equal(t, Vote112B, got)
	})

	t.Run("unknown value is an error naming the menu", func(t *testing.T) {
		_, err := Parse("vote-index2")
		require.Error(t, err)
		require.Contains(t, err.Error(), "vote-index2")
		require.Contains(t, err.Error(), "vote-index-2")
	})
}

func TestActiveAndIs(t *testing.T) {
	SetForTest(t, None) // don't depend on test execution order leaving Active() at None already

	require.Equal(t, None, Active())
	require.False(t, Enabled())
	require.False(t, Is(Vote112B))

	SetForTest(t, Vote112B)
	require.Equal(t, Vote112B, Active())
	require.True(t, Enabled())
	require.True(t, Is(Vote112B))
	require.False(t, Is(VoteIndex2))
}
