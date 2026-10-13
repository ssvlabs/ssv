package exporter

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func TestValidateSlotRange(t *testing.T) {
	tests := []struct {
		name    string
		from    uint64
		to      uint64
		wantErr string
	}{
		{name: "single slot", from: 7, to: 7},
		{name: "ascending range", from: 1, to: 100},
		{name: "largest terminating 'to'", from: 1, to: math.MaxUint64 - 1},
		{name: "from greater than to", from: 10, to: 5, wantErr: "'from' must be less than or equal to 'to'"},
		{name: "'to' of max uint64 would wrap the inclusive loop", from: 1, to: math.MaxUint64, wantErr: "'to' must be less than"},
		{name: "'from' and 'to' both max uint64", from: math.MaxUint64, to: math.MaxUint64, wantErr: "'to' must be less than"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSlotRange(tt.from, tt.to)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// The committee and decideds endpoints run the same inclusive per-slot loop as
// /traces/validator, so their validators must share the max-uint64 guard.
func TestValidateCommitteeRequest_SlotRange(t *testing.T) {
	require.NoError(t, validateCommitteeRequest(&CommitteeTracesQuery{From: 1, To: 2}))
	require.ErrorContains(t, validateCommitteeRequest(&CommitteeTracesQuery{From: 2, To: 1}), "'from' must be less than or equal to 'to'")
	require.ErrorContains(t, validateCommitteeRequest(&CommitteeTracesQuery{From: 1, To: math.MaxUint64}), "'to' must be less than")
}

func TestValidateDecidedRequest_SlotRange(t *testing.T) {
	roles := []spectypes.BeaconRole{spectypes.BNRoleProposer}
	require.NoError(t, validateDecidedRequest(&DecidedsQuery{From: 1, To: 2, Roles: roles}))
	require.ErrorContains(t, validateDecidedRequest(&DecidedsQuery{From: 2, To: 1, Roles: roles}), "'from' must be less than or equal to 'to'")
	require.ErrorContains(t, validateDecidedRequest(&DecidedsQuery{From: 1, To: math.MaxUint64, Roles: roles}), "'to' must be less than")
}
