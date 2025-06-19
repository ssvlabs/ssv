package testutils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/sourcegraph/conc/pool"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// RandomSuffix generates a random hex string suffix for unique resource naming
func RandomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if random fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ConcurrentSignResult represents the result of a concurrent signing operation
type ConcurrentSignResult struct {
	Sig  spectypes.Signature
	Root phase0.Root
	Err  error
	ID   int
}

// RunConcurrentSigning is a helper function to test concurrent signing with proper synchronization
// using the conc/pool library for improved ergonomics and safety.
func RunConcurrentSigning(
	t *testing.T,
	ctx context.Context,
	numGoroutines int,
	targetName string,
	signFunc func() (spectypes.Signature, phase0.Root, error),
) int {
	req := require.New(t)

	p := pool.NewWithResults[*ConcurrentSignResult]().WithContext(ctx)

	// Submit all signing tasks
	for i := 0; i < numGoroutines; i++ {
		goroutineID := i // Capture loop variable
		p.Go(func(ctx context.Context) (*ConcurrentSignResult, error) {
			sig, root, err := signFunc()
			return &ConcurrentSignResult{
				Sig:  sig,
				Root: root,
				Err:  err,
				ID:   goroutineID,
			}, nil // Pool handles task errors separately from signing errors
		})
	}

	// Wait for all tasks to complete
	results, err := p.Wait()
	if err != nil {
		t.Fatalf("%s concurrent test failed with pool error: %v", targetName, err)
	}

	// Analyze results
	var successfulSigs []spectypes.Signature
	var successfulRoots []phase0.Root
	successCount := 0

	for _, result := range results {
		if result.Err == nil {
			successCount++
			successfulSigs = append(successfulSigs, result.Sig)
			successfulRoots = append(successfulRoots, result.Root)
			t.Logf("%s: Goroutine %d succeeded", targetName, result.ID)
		} else {
			t.Logf("%s: Goroutine %d failed with error: %v", targetName, result.ID, result.Err)
		}
	}

	t.Logf("%s: %d successful signatures out of %d attempts", targetName, successCount, numGoroutines)

	// Verify all successful signatures are identical (deterministic signing)
	if successCount > 1 {
		firstSig := successfulSigs[0]
		firstRoot := successfulRoots[0]
		for i := 1; i < len(successfulSigs); i++ {
			req.Equal(firstSig, successfulSigs[i], "%s: All successful signatures should be identical", targetName)
			req.Equal(firstRoot, successfulRoots[i], "%s: All successful roots should be identical", targetName)
		}
	}

	return successCount
}
