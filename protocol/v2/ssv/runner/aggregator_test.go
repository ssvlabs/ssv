package runner

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// Test_isAggregatorFn checks that the default IsAggregator func (delegating to the shared
// networkconfig.IsAggregatorSelected helper) is deterministic (in concurrent setting).
func Test_isAggregatorFn(t *testing.T) {
	const targetAggregatorsPerCommittee = 3
	const committeeLength = 10

	slotSig := []byte(randStringBytes(64))

	isAggFn := isAggregatorFn()
	sampleResult := isAggFn(targetAggregatorsPerCommittee, committeeLength, slotSig)

	const goRoutines = 1000
	results := make(chan bool)
	for range goRoutines {
		go func() {
			result := isAggFn(targetAggregatorsPerCommittee, committeeLength, slotSig)
			results <- result
		}()
	}
	for range goRoutines {
		result := <-results
		require.Equal(t, sampleResult, result)
	}
}

// Test_isAggregatorFn_MatchesSharedHelper asserts the runner's default IsAggregator func is a
// thin, bit-identical wrapper over the shared networkconfig.IsAggregatorSelected helper — the
// same helper beacon/goclient.GoClient.IsAggregator delegates to — across a spread of committee
// sizes (including committeeLength < target, which clamps modulo to 1).
//
// Note: today isAggregatorFn returns networkconfig.IsAggregatorSelected verbatim, so both sides
// invoke the same function value and this assertion is trivially true. The test exists purely as
// a regression guard: it fails the day the runner's default stops delegating to the shared helper
// (or wraps it with anything that changes the result).
func Test_isAggregatorFn_MatchesSharedHelper(t *testing.T) {
	isAggFn := isAggregatorFn()

	slotSigs := [][]byte{
		[]byte(randStringBytes(64)),
		[]byte(randStringBytes(64)),
		bytes64(0xAA),
		bytes64(0xBB),
	}

	targets := []uint64{1, 3, 16}
	committeeLengths := []uint64{0, 1, 2, 10, 16, 128}

	for _, target := range targets {
		for _, committeeLength := range committeeLengths {
			for _, slotSig := range slotSigs {
				want := networkconfig.IsAggregatorSelected(target, committeeLength, slotSig)
				got := isAggFn(target, committeeLength, slotSig)
				require.Equal(t, want, got, "target=%d committeeLength=%d", target, committeeLength)
			}
		}
	}
}

func bytes64(b byte) []byte {
	out := make([]byte, 64)
	for i := range out {
		out[i] = b
	}
	return out
}

func randStringBytes(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
