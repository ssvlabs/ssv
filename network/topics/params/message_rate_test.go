package params

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_calcMsgRateForTopic(t *testing.T) {
	tenThousandCommittees := make([]int, 10000)
	tenThousandValidators := make([]int, 10000)
	for i := range tenThousandCommittees {
		tenThousandCommittees[i] = 4
		tenThousandValidators[i] = 1
	}

	type args struct {
		committeeSizes  []int
		validatorCounts []int
	}
	tests := []struct {
		name string
		args args
		want float64
	}{
		{
			name: "Case 1",
			args: args{
				committeeSizes:  []int{4, 4},
				validatorCounts: []int{500, 500},
			},
			want: 1622.4707248475067,
		},
		{
			name: "Case 2",
			args: args{
				committeeSizes:  tenThousandCommittees,
				validatorCounts: tenThousandValidators,
			},
			want: 159005.50819672088,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, calcMsgRateForTopic(tt.args.committeeSizes, tt.args.validatorCounts), tt.want*0.001)
		})
	}
}
