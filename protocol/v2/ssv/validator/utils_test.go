package validator

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRemoveIndices(t *testing.T) {
	type TestInputType struct {
		duties          []*types.BeaconDuty
		indicesToRemove []int
	}
	type TestCase struct {
		input             TestInputType
		output            []int
		expectedErrorText string
	}

	testCases := []TestCase{
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2}, {Slot: 3}, {Slot: 4},
				},
				indicesToRemove: []int{0, 3, 1},
			},
			output:            []int{2, 4},
			expectedErrorText: "",
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 1},
				},
				indicesToRemove: []int{0},
			},
			output:            []int{},
			expectedErrorText: "",
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2}, {Slot: 3},
				},
				indicesToRemove: []int{0, 3},
			},
			output:            []int{1, 2},
			expectedErrorText: "",
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2}, {Slot: 3},
				},
				indicesToRemove: []int{0, 3, 3, 3},
			},
			output:            []int{},
			expectedErrorText: "duplicate index 3 in [0 3 3 3]",
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2}, {Slot: 3},
				},
				indicesToRemove: []int{0, 23, 42},
			},
			output:            []int{},
			expectedErrorText: "index 23 out of range of slice with length 4",
		},
	}

	for _, tc := range testCases {
		filteredDuties, err := removeIndices(tc.input.duties, tc.input.indicesToRemove)

		if tc.expectedErrorText != "" {
			require.Equal(t, tc.expectedErrorText, err.Error())
			continue
		}

		slotsLeft := make([]int, 0)
		for _, v := range filteredDuties {
			slotsLeft = append(slotsLeft, int(v.Slot))
		}

		require.Len(t, slotsLeft, len(tc.output))
		require.ElementsMatch(t, tc.output, slotsLeft)
	}
}
