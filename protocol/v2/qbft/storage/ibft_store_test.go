package qbftstorage

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a slice of OperatorIDs.
func createOperatorIDs(ids ...uint64) []spectypes.OperatorID {
	if len(ids) == 0 {
		return make([]spectypes.OperatorID, 0)
	}
	return ids
}

// Helper function to create a Quorum instance.
func newQuorum(signers, committee []spectypes.OperatorID) Quorum {
	return Quorum{
		Signers:   signers,
		Committee: committee,
	}
}

func TestQuorum_ToBitMask_NormalCases(t *testing.T) {
	tests := []struct {
		name     string
		quorum   Quorum
		expected SignersBitMask
	}{
		{
			name: "Single common element",
			quorum: newQuorum(
				createOperatorIDs(1),
				createOperatorIDs(1, 2, 3, 4),
			),
			expected: 1 << 0, // 0b0000000000000001
		},
		{
			name: "Multiple common elements",
			quorum: newQuorum(
				createOperatorIDs(1, 2),
				createOperatorIDs(1, 2, 3, 4),
			),
			expected: (1 << 0) | (1 << 1), // 0b0000000000000011
		},
		{
			name: "All committee members are signers",
			quorum: newQuorum(
				createOperatorIDs(1, 2, 3, 4),
				createOperatorIDs(1, 2, 3, 4),
			),
			expected: (1 << 0) | (1 << 1) | (1 << 2) | (1 << 3), // 0b0000000000001111
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.quorum.ToSignersBitMask()
			assert.Equal(t, tt.expected, result, "Bitmask does not match expected value")
		})
	}
}

func TestQuorum_ToBitMask_InvalidSizes(t *testing.T) {
	tests := []struct {
		name        string
		quorum      Quorum
		shouldPanic bool
	}{
		{
			name: "Committee size exceeds maxCommitteeSize",
			quorum: newQuorum(
				createOperatorIDs(1, 2, 3),
				func() []spectypes.OperatorID {
					committee := make([]spectypes.OperatorID, maxCommitteeSize+1)
					for i := 0; i < maxCommitteeSize+1; i++ {
						committee[i] = spectypes.OperatorID(i + 1)
					}
					return committee
				}(),
			),
			shouldPanic: true,
		},
		{
			name: "Signers size exceeds maxCommitteeSize",
			quorum: newQuorum(
				func() []spectypes.OperatorID {
					signers := make([]spectypes.OperatorID, maxCommitteeSize+1)
					for i := 0; i < maxCommitteeSize+1; i++ {
						signers[i] = spectypes.OperatorID(i + 1)
					}
					return signers
				}(),
				createOperatorIDs(1),
			),
			shouldPanic: true,
		},
		{
			name: "Signers size greater than Committee size",
			quorum: newQuorum(
				createOperatorIDs(1, 2, 3),
				createOperatorIDs(1, 2),
			),
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				require.Panics(t, func() { _ = tt.quorum.ToSignersBitMask() }, "Expected ToSignersBitMask to panic")
			} else {
				require.NotPanics(t, func() { _ = tt.quorum.ToSignersBitMask() }, "Did not expect ToSignersBitMask to panic")
			}
		})
	}
}

func TestQuorum_ToBitMask_EmptyArrays(t *testing.T) {
	tests := []struct {
		name     string
		quorum   Quorum
		expected SignersBitMask
	}{
		{
			name: "Both Signers and Committee are empty",
			quorum: newQuorum(
				createOperatorIDs(),
				createOperatorIDs(),
			),
			expected: 0, // 0b0000000000000000
		},
		{
			name: "Signers empty, Committee non-empty",
			quorum: newQuorum(
				createOperatorIDs(),
				createOperatorIDs(1, 2),
			),
			expected: 0, // 0b0000000000000000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.quorum.ToSignersBitMask()
			assert.Equal(t, tt.expected, result, "Bitmask should be 0 when one or both arrays are empty")
		})
	}
}

func TestOperatorsBitMask_Signers_NormalCases(t *testing.T) {
	tests := []struct {
		name      string
		bitmask   SignersBitMask
		committee []spectypes.OperatorID
		expected  []spectypes.OperatorID
	}{
		{
			name:      "Single bit set",
			bitmask:   1 << 0, // Bit 0 set
			committee: createOperatorIDs(1, 2, 3, 4),
			expected:  createOperatorIDs(1),
		},
		{
			name:      "Multiple bits set",
			bitmask:   (1 << 0) | (1 << 2) | (1 << 4), // Bits 0, 2, 4 set
			committee: createOperatorIDs(1, 2, 3, 4, 5, 6, 7),
			expected:  createOperatorIDs(1, 3, 5),
		},
		{
			name:      "All bits set",
			bitmask:   0xFFFF, // All bits set
			committee: createOperatorIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13),
			expected:  createOperatorIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13),
		},
		{
			name:      "No bits set",
			bitmask:   0, // No bits set
			committee: createOperatorIDs(1, 2, 3),
			expected:  createOperatorIDs(),
		},
		{
			name:      "Partial overlap",
			bitmask:   (1 << 1) | (1 << 3), // Bits 1 and 3 set
			committee: createOperatorIDs(10, 20, 30, 40),
			expected:  createOperatorIDs(20, 40),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.bitmask.Signers(tt.committee)
			require.NoError(t, err, "Signers should not return an error")
			assert.Equal(t, tt.expected, result, "Signers do not match expected values")
		})
	}
}

func TestOperatorsBitMask_Signers_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		bitmask   SignersBitMask
		committee []spectypes.OperatorID
		expected  []spectypes.OperatorID
	}{
		{
			name:      "Empty committee",
			bitmask:   1 << 0, // Bit 0 set
			committee: createOperatorIDs(),
			expected:  createOperatorIDs(),
		},
		{
			name:      "Empty bitmask and committee",
			bitmask:   0,
			committee: createOperatorIDs(),
			expected:  createOperatorIDs(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.bitmask.Signers(tt.committee)
			require.NoError(t, err, "Signers should not return an error")
			assert.Equal(t, tt.expected, result, "Signers do not match expected values")
		})
	}
}

func TestOperatorsBitMask_Signers_InvalidSizes(t *testing.T) {
	tests := []struct {
		name        string
		bitmask     SignersBitMask
		committee   []spectypes.OperatorID
		shouldError bool
	}{
		{
			name:    "Committee size exceeds maxCommitteeSize",
			bitmask: 1 << 0,
			committee: func() []spectypes.OperatorID {
				committee := make([]spectypes.OperatorID, maxCommitteeSize+1)
				for i := 0; i < maxCommitteeSize+1; i++ {
					committee[i] = spectypes.OperatorID(i + 1)
				}
				return committee
			}(),
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldError {
				_, err := tt.bitmask.Signers(tt.committee)
				require.Error(t, err, "Expected Signers to return an error")
			} else {
				_, err := tt.bitmask.Signers(tt.committee)
				require.NoError(t, err, "Did not expect Signers to panic")
			}
		})
	}
}

func TestOperatorsBitMask_Signers_PartialCommittee(t *testing.T) {
	quorum := newQuorum(
		createOperatorIDs(1, 2, 3, 4, 5),
		createOperatorIDs(1, 3, 5, 7, 9),
	)
	// Bitmask: bits 0,2,4 set (corresponding to committee indices 0,2,4)
	bitmask := SignersBitMask((1 << 0) | (1 << 2) | (1 << 4))
	expected := createOperatorIDs(1, 5, 9)

	result, err := bitmask.Signers(quorum.Committee)
	require.NoError(t, err, "Signers should not return an error")
	assert.Equal(t, expected, result, "Expected signers do not match the bitmask")
}

func TestOperatorsBitMask_Signers_LargeOperatorIDs(t *testing.T) {
	committee := createOperatorIDs(100, 200, 2000)
	// Bitmask: bits 0 and 2 set
	bitmask := SignersBitMask((1 << 0) | (1 << 2))
	expected := createOperatorIDs(100, 2000)

	result, err := bitmask.Signers(committee)
	require.NoError(t, err, "Signers should not return an error")
	assert.Equal(t, expected, result, "Signers with large OperatorIDs should be correctly returned")
}
