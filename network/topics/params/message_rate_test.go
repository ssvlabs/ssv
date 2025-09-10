package params

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
)

func createTestingValidators(n int) []*types.SSVShare {
	ret := make([]*types.SSVShare, 0)
	for i := 1; i <= n; i++ {
		ret = append(ret, &types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex: phase0.ValidatorIndex(i),
			},
		})
	}
	return ret
}

func createTestingSingleCommittees(n uint64) []*storage.Committee {
	ret := make([]*storage.Committee, 0)
	for i := uint64(0); i <= n; i++ {
		opRef := i*4 + 1
		ret = append(ret, &storage.Committee{
			Operators: []uint64{opRef, opRef + 1, opRef + 2, opRef + 3},
			Shares:    createTestingValidators(1),
		})
	}
	return ret
}

func TestCalculateMessageRateForTopic(t *testing.T) {
	tenThousandCommittees := make([]int, 10000)
	tenThousandValidators := make([]int, 10000)
	for i := range tenThousandCommittees {
		tenThousandCommittees[i] = 4
		tenThousandValidators[i] = 1
	}

	type args struct {
		committees []*storage.Committee
	}
	tests := []struct {
		name string
		args args
		want float64
	}{
		{
			name: "Case 1",
			args: args{
				committees: []*storage.Committee{
					{
						Operators: []uint64{1, 2, 3, 4},
						Shares:    createTestingValidators(500),
					},
					{
						Operators: []uint64{5, 6, 7, 8},
						Shares:    createTestingValidators(500),
					},
				},
			},
			want: 4.2242497530608745,
		},
		{
			name: "Case 2",
			args: args{
				committees: createTestingSingleCommittees(10000),
			},
			want: 414.1089067500509,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := newRateCalculator(networkconfig.TestNetwork)
			msgRate := rc.calculateMessageRateForTopic(tt.args.committees)
			require.InDelta(t, tt.want, msgRate, tt.want*0.001)
		})
	}
}
