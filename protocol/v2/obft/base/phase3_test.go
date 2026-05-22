package base

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSelectWinningGroup verifies the σ-group selection helper:
// determinism-load-bearing lex-tiebreak on V when partial counts are
// equal. Without the tiebreak, equal-count groups would resolve based
// on slice-iteration order (here always input order, but the helper is
// agnostic), producing nondeterministic Output across operators on
// transient pre-quorum states.
func TestSelectWinningGroup(t *testing.T) {
	mkPartials := func(ops ...OperatorID) map[OperatorID]Signature {
		out := make(map[OperatorID]Signature, len(ops))
		for _, op := range ops {
			out[op] = Signature{byte(op)}
		}
		return out
	}

	t.Run("empty groups returns nil", func(t *testing.T) {
		require.Nil(t, selectWinningGroup(nil))
		require.Nil(t, selectWinningGroup([]*sigGroup{}))
	})

	t.Run("single group wins", func(t *testing.T) {
		g := &sigGroup{value: Value("V0"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, g, selectWinningGroup([]*sigGroup{g}))
	})

	t.Run("higher count wins regardless of V order", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, gB, selectWinningGroup([]*sigGroup{gA, gB}))
		require.Equal(t, gB, selectWinningGroup([]*sigGroup{gB, gA}))
	})

	t.Run("equal count: lex-smaller V wins (forward order)", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(3, 4)}
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gA, gB}))
	})

	t.Run("equal count: lex-smaller V wins (reverse input order)", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(3, 4)}
		// Same V's, reversed input order — winner must still be V_a.
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gB, gA}))
	})

	t.Run("equal count three-way: smallest V wins", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(2)}
		gC := &sigGroup{value: Value("V_c"), partials: mkPartials(3)}
		// Permutations should all yield gA.
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gA, gB, gC}))
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gC, gB, gA}))
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gB, gC, gA}))
	})

	t.Run("byte-level lex order (not lexicographic-by-length)", func(t *testing.T) {
		// "AB" < "AC" byte-wise.
		gAB := &sigGroup{value: Value("AB"), partials: mkPartials(1)}
		gAC := &sigGroup{value: Value("AC"), partials: mkPartials(2)}
		require.Equal(t, gAB, selectWinningGroup([]*sigGroup{gAB, gAC}))
		require.Equal(t, gAB, selectWinningGroup([]*sigGroup{gAC, gAB}))
	})
}
