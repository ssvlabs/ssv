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
		input  TestInputType
		output []int
	}

	testCases := []TestCase{
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2}, {Slot: 3}, {Slot: 4},
				},
				indicesToRemove: []int{0, 3, 1},
			},
			output: []int{2, 4},
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 1},
				},
				indicesToRemove: []int{0},
			},
			output: []int{},
		},
	}

	for _, tc := range testCases {
		filteredDuties := removeIndices(tc.input.duties, tc.input.indicesToRemove)
		slotsLeft := make([]int, 0)
		for _, v := range filteredDuties {
			slotsLeft = append(slotsLeft, int(v.Slot))
		}

		require.Len(t, slotsLeft, len(tc.output))
		require.ElementsMatch(t, tc.output, slotsLeft)
	}
}
