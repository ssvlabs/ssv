package validator

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRemoveIndecies(t *testing.T) {
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
					{Slot: 1}, {Slot: 2}, {Slot: 3}, {Slot: 4}, {Slot: 5},
				},
				indicesToRemove: []int{0, 3, 1},
			},
			output: []int{3, 5},
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 1},
				},
				indicesToRemove: []int{0, 1, 2},
			},
			output: []int{},
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2},
				},
				indicesToRemove: []int{1, 1, 1},
			},
			output: []int{0, 2},
		},
		{
			input: TestInputType{
				duties: []*types.BeaconDuty{
					{Slot: 0}, {Slot: 1}, {Slot: 2},
				},
				indicesToRemove: []int{2, 42},
			},
			output: []int{0, 1},
		},
	}

	for _, tc := range testCases {
		filteredDuties := removeIndices(tc.input.duties, tc.input.indicesToRemove)
		ids := make([]int, 0)
		for _, v := range filteredDuties {
			ids = append(ids, int(v.Slot))
		}

		require.Len(t, ids, len(tc.output))
		require.ElementsMatch(t, tc.output, ids)
	}
}
