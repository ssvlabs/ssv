package validation

import (
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/networkconfig"
)

func TestMessageValidator_currentEstimatedRound(t *testing.T) {
	tt := []struct {
		name           string
		role           spectypes.BeaconRole
		sinceSlotStart time.Duration
		want           specqbft.Round
	}{
		{
			name:           "0s - expected round 1",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 0,
			want:           specqbft.FirstRound,
		},
		{
			name:           "5s - expected round 1",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 5 * time.Second,
			want:           specqbft.FirstRound,
		},
		{
			name:           "6s - expected round 2",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 6 * time.Second,
			want:           specqbft.Round(2),
		},
		{
			name:           "8s - expected round 3",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 8 * time.Second,
			want:           specqbft.Round(3),
		},
		{
			name:           "10s - expected round 4",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 10 * time.Second,
			want:           specqbft.Round(4),
		},
		{
			name:           "12s - expected round 5",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 12 * time.Second,
			want:           specqbft.Round(5),
		},
		{
			name:           "14s - expected round 6",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 14 * time.Second,
			want:           specqbft.Round(6),
		},
		{
			name:           "16s - expected round 7",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 16 * time.Second,
			want:           specqbft.Round(7),
		},
		{
			name:           "18s - expected round 8",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 18 * time.Second,
			want:           specqbft.Round(8),
		},
		{
			name:           "20s - expected round 9",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 20 * time.Second,
			want:           specqbft.Round(9),
		},
		{
			name:           "2m20s - expected round 10",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 2*time.Minute + 20*time.Second,
			want:           specqbft.Round(10),
		},
		{
			name:           "4m20s - expected round 11",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 4*time.Minute + 20*time.Second,
			want:           specqbft.Round(11),
		},
		{
			name:           "6m20s - expected round 12",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 6*time.Minute + 20*time.Second,
			want:           specqbft.Round(12),
		},
		{
			name:           "8m20s - expected round 13",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 8*time.Minute + 20*time.Second,
			want:           specqbft.Round(13),
		},
		{
			name:           "99999s - expected round 13",
			role:           spectypes.BNRoleAttester,
			sinceSlotStart: 99999 * time.Second,
			want:           specqbft.Round(13),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mv := NewMessageValidator(networkconfig.TestNetwork).(*messageValidator)

			got := mv.roundThresholdCache.EstimatedRound(tc.role, tc.sinceSlotStart)
			require.Equal(t, tc.want, got)
		})
	}
}
