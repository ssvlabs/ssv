package kms

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNonceReplaceWait(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	// Saved before times were kept: free at once.
	require.Zero(t, nonceReplaceWait(0, now))
	// Just signed: held for nonceReplaceAfter.
	require.Equal(t, nonceReplaceAfter, nonceReplaceWait(now.Unix(), now))
	require.Equal(t, nonceReplaceAfter-10*time.Second, nonceReplaceWait(now.Unix()-10, now))
	// Long enough ago: free.
	require.LessOrEqual(t, nonceReplaceWait(now.Unix()-int64(nonceReplaceAfter.Seconds()), now), time.Duration(0))
	require.Negative(t, nonceReplaceWait(now.Unix()-3600, now))
}
