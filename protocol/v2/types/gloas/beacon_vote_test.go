package gloas

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestGloasBeaconVote_SSZRoundTrip(t *testing.T) {
	vote := &GloasBeaconVote{
		BlockRoot:            phase0.Root{0x01, 0x02, 0x03},
		Source:               &phase0.Checkpoint{Epoch: 10, Root: phase0.Root{0xaa}},
		Target:               &phase0.Checkpoint{Epoch: 11, Root: phase0.Root{0xbb}},
		AttestationDataIndex: 1,
	}

	// Fixed 120-byte encoding (112B BeaconVote layout + 8B AttestationDataIndex).
	require.Equal(t, 120, vote.SizeSSZ())

	enc, err := vote.Encode()
	require.NoError(t, err)
	require.Len(t, enc, 120)

	var decoded GloasBeaconVote
	require.NoError(t, decoded.Decode(enc))
	require.Equal(t, vote.BlockRoot, decoded.BlockRoot)
	require.Equal(t, vote.Source, decoded.Source)
	require.Equal(t, vote.Target, decoded.Target)
	require.Equal(t, vote.AttestationDataIndex, decoded.AttestationDataIndex)

	htr1, err := vote.HashTreeRoot()
	require.NoError(t, err)
	htr2, err := decoded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, htr1, htr2)
}

// TestGloasBeaconVote_CrossForkDecodeFails asserts a 120B Gloas vote cannot be
// decoded from a 112B (pre-Gloas BeaconVote) buffer, so fork selection by length
// fails cleanly.
func TestGloasBeaconVote_CrossForkDecodeFails(t *testing.T) {
	var v GloasBeaconVote
	require.Error(t, v.Decode(make([]byte, 112)))
}
