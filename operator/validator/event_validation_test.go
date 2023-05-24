package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/eth1/abiparser"
)

func Test_validateValidatorAddedEvent(t *testing.T) {
	generateOperatorIDs := func(count uint64) []uint64 {
		result := make([]uint64, 0)
		for i := uint64(1); i <= count; i++ {
			result = append(result, i)
		}
		return result
	}
	tt := []struct {
		name  string
		event abiparser.ValidatorAddedEvent
		err   string
	}{
		{
			name: "4 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(4),
			},
			err: "",
		},
		{
			name: "7 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(7),
			},
			err: "",
		},
		{
			name: "10 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(10),
			},
			err: "",
		},
		{
			name: "13 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(13),
			},
			err: "",
		},
		{
			name: "14 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(14),
			},
			err: "too many operator IDs: 14",
		},
		{
			name: "0 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(0),
			},
			err: "no operators",
		},
		{
			name: "1 operator",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(1),
			},
			err: "given operator count (1) cannot build a 3f+1 quorum",
		},
		{
			name: "2 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(2),
			},
			err: "given operator count (2) cannot build a 3f+1 quorum",
		},
		{
			name: "3 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(3),
			},
			err: "given operator count (3) cannot build a 3f+1 quorum",
		},
		{
			name: "5 operators",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: generateOperatorIDs(5),
			},
			err: "given operator count (5) cannot build a 3f+1 quorum",
		},
		{
			name: "duplicated operator",
			event: abiparser.ValidatorAddedEvent{
				OperatorIds: []uint64{1, 2, 3, 3},
			},
			err: "duplicated operator ID: 3",
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateValidatorAddedEvent(tc.event)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
