package validation

import (
	"testing"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

func TestMessageValidator_currentEstimatedRound(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	tt := []struct {
		name           string
		role           spectypes.RunnerRole
		sinceSlotStart time.Duration
		want           specqbft.Round
	}{
		{
			name:           "0s - expected first round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: 0,
			want:           specqbft.FirstRound,
		},
		{
			name:           "QuickTimeout/2 - expected first round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout / 2,
			want:           specqbft.FirstRound,
		},
		{
			name:           "QuickTimeout - expected first+1 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout,
			want:           specqbft.FirstRound + 1,
		},
		{
			name:           "QuickTimeout*2 - expected first+2 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 2,
			want:           specqbft.FirstRound + 2,
		},
		{
			name:           "QuickTimeout*3 - expected first+3 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 3,
			want:           specqbft.FirstRound + 3,
		},
		{
			name:           "QuickTimeout*4 - expected first+4 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 4,
			want:           specqbft.FirstRound + 4,
		},
		{
			name:           "QuickTimeout*5 - expected first+5 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 5,
			want:           specqbft.FirstRound + 5,
		},
		{
			name:           "QuickTimeout*6 - expected first+6 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 6,
			want:           specqbft.FirstRound + 6,
		},
		{
			name:           "QuickTimeout*7 - expected first+7 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 7,
			want:           specqbft.FirstRound + 7,
		},
		{
			name:           "QuickTimeout*8 - expected first+8 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * 8,
			want:           specqbft.FirstRound + 8,
		},
		{
			name:           "QuickTimeout*9 - expected first+8 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+1),
			want:           roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:           "QuickTimeout*10 - expected first+8 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+2),
			want:           roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout) - expected first+9 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout,
			want:           roundtimer.QuickTimeoutThreshold + 2,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout*2) - expected first+10 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*2,
			want:           roundtimer.QuickTimeoutThreshold + 3,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout*3) - expected first+11 round",
			role:           spectypes.RoleProposer,
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*3,
			want:           roundtimer.QuickTimeoutThreshold + 4,
		},
		{
			name:           "committee uses one-third slot delay before round timeouts start",
			role:           spectypes.RoleCommittee,
			sinceSlotStart: netCfg.SlotDuration / 3,
			want:           specqbft.FirstRound,
		},
		{
			name:           "committee round advances after one-third slot delay plus quick timeout",
			role:           spectypes.RoleCommittee,
			sinceSlotStart: netCfg.SlotDuration/3 + roundtimer.QuickTimeout,
			want:           specqbft.FirstRound + 1,
		},
		{
			name:           "aggregator uses two-third slot delay before round timeouts start",
			role:           spectypes.RoleAggregator,
			sinceSlotStart: netCfg.SlotDuration / 3 * 2,
			want:           specqbft.FirstRound,
		},
		{
			name:           "sync committee contribution advances after two-third slot delay plus quick timeout",
			role:           spectypes.RoleSyncCommitteeContribution,
			sinceSlotStart: netCfg.SlotDuration/3*2 + roundtimer.QuickTimeout,
			want:           specqbft.FirstRound + 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			mv := &messageValidator{netCfg: netCfg}
			got, err := mv.currentEstimatedRound(tc.role, tc.sinceSlotStart)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLowestAllowedRound(t *testing.T) {
	tt := []struct {
		name           string
		estimatedRound specqbft.Round
		want           specqbft.Round
	}{
		{
			name:           "first round stays at first round",
			estimatedRound: specqbft.FirstRound,
			want:           specqbft.FirstRound,
		},
		{
			name:           "round before first-round boundary is clamped",
			estimatedRound: specqbft.FirstRound + allowedRoundsInPast - 1,
			want:           specqbft.FirstRound,
		},
		{
			name:           "boundary at first round still returns first round",
			estimatedRound: specqbft.FirstRound + allowedRoundsInPast,
			want:           specqbft.FirstRound,
		},
		{
			name:           "later rounds keep the allowed past spread",
			estimatedRound: specqbft.FirstRound + allowedRoundsInPast + 3,
			want:           specqbft.FirstRound + 3,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, lowestAllowedRound(tc.estimatedRound))
		})
	}
}

func TestMessageValidator_roundBelongsToAllowedSpread(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}
	slot := netCfg.FirstSlotAtEpoch(1)
	signedSSVMessage := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgID: spectypes.NewMsgID(netCfg.DomainType, make([]byte, 48), spectypes.RoleProposer),
		},
	}

	tt := []struct {
		name           string
		sinceSlotStart time.Duration
		round          specqbft.Round
		wantErr        error
	}{
		{
			name:           "first round is still allowed at the lower bound clamp",
			sinceSlotStart: roundtimer.QuickTimeout * allowedRoundsInPast,
			round:          specqbft.FirstRound,
		},
		{
			name:           "rounds older than the allowed past spread are rejected",
			sinceSlotStart: roundtimer.QuickTimeout * 5,
			round:          specqbft.FirstRound + 2,
			wantErr:        ErrEstimatedRoundNotInAllowedSpread,
		},
		{
			name:           "lowest round in the allowed past spread is accepted",
			sinceSlotStart: roundtimer.QuickTimeout * 5,
			round:          specqbft.FirstRound + 3,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := mv.roundBelongsToAllowedSpread(
				signedSSVMessage,
				&specqbft.Message{
					Height: specqbft.Height(slot),
					Round:  tc.round,
				},
				netCfg.SlotStartTime(slot).Add(tc.sinceSlotStart),
			)

			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
