package runner

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_isAggregatorFn checks that isAggregatorFn is deterministic (in concurrent setting).
func Test_isAggregatorFn(t *testing.T) {
	const targetAggregatorsPerCommittee = 3
	const committeeCount = 10

	slotSig := []byte(randStringBytes(64))

	isAggFn := isAggregatorFn()
	sampleResult := isAggFn(targetAggregatorsPerCommittee, committeeCount, slotSig)

	const goRoutines = 1000
	results := make(chan bool)
	for i := 0; i < goRoutines; i++ {
		go func() {
			result := isAggFn(targetAggregatorsPerCommittee, committeeCount, slotSig)
			results <- result
		}()
	}
	for i := 0; i < goRoutines; i++ {
		result := <-results
		require.Equal(t, sampleResult, result)
	}
}

func randStringBytes(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
