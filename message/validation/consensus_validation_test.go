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
		name         string
		role         spectypes.RunnerRole
		timeIntoSlot time.Duration
		want         specqbft.Round
	}{
		{
			name:         "0s - expected first round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: 0,
			want:         specqbft.FirstRound,
		},
		{
			name:         "QuickTimeout/2 - expected first round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout / 2,
			want:         specqbft.FirstRound,
		},
		{
			name:         "QuickTimeout - expected first+1 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
		{
			name:         "QuickTimeout*2 - expected first+2 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 2,
			want:         specqbft.FirstRound + 2,
		},
		{
			name:         "QuickTimeout*3 - expected first+3 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 3,
			want:         specqbft.FirstRound + 3,
		},
		{
			name:         "QuickTimeout*4 - expected first+4 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 4,
			want:         specqbft.FirstRound + 4,
		},
		{
			name:         "QuickTimeout*5 - expected first+5 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 5,
			want:         specqbft.FirstRound + 5,
		},
		{
			name:         "QuickTimeout*6 - expected first+6 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 6,
			want:         specqbft.FirstRound + 6,
		},
		{
			name:         "QuickTimeout*7 - expected first+7 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 7,
			want:         specqbft.FirstRound + 7,
		},
		{
			name:         "QuickTimeout*8 - expected first+8 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 8,
			want:         specqbft.FirstRound + 8,
		},
		{
			name:         "QuickTimeout*9 - expected first+8 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+1),
			want:         roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:         "QuickTimeout*10 - expected first+8 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+2),
			want:         roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:         "(QuickTimeout*8 + SlowTimeout) - expected first+9 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout,
			want:         roundtimer.QuickTimeoutThreshold + 2,
		},
		{
			name:         "(QuickTimeout*8 + SlowTimeout*2) - expected first+10 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*2,
			want:         roundtimer.QuickTimeoutThreshold + 3,
		},
		{
			name:         "(QuickTimeout*8 + SlowTimeout*3) - expected first+11 round",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*3,
			want:         roundtimer.QuickTimeoutThreshold + 4,
		},
		{
			name:         "committee uses one-third slot delay before round timeouts start",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: netCfg.SlotDuration / 3,
			want:         specqbft.FirstRound,
		},
		{
			name:         "committee round advances after one-third slot delay plus quick timeout",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: netCfg.SlotDuration/3 + roundtimer.QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
		{
			name:         "aggregator uses two-third slot delay before round timeouts start",
			role:         spectypes.RoleAggregator,
			timeIntoSlot: netCfg.SlotDuration / 3 * 2,
			want:         specqbft.FirstRound,
		},
		{
			name:         "sync committee contribution advances after two-third slot delay plus quick timeout",
			role:         spectypes.RoleSyncCommitteeContribution,
			timeIntoSlot: netCfg.SlotDuration/3*2 + roundtimer.QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			mv := &messageValidator{netCfg: netCfg}
			got, err := mv.currentEstimatedRound(tc.role, tc.timeIntoSlot)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
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
		name         string
		role         spectypes.RunnerRole
		timeIntoSlot time.Duration
		round        specqbft.Round
		wantErr      error
	}{
		{
			name:         "first round is still allowed at the lower bound clamp",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: roundtimer.QuickTimeout * allowedRoundsInPast,
			round:        specqbft.FirstRound,
		},
		{
			name:         "rounds older than the allowed past spread are rejected",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: netCfg.SlotDuration/3 + roundtimer.QuickTimeout*5,
			round:        specqbft.FirstRound + 2,
			wantErr:      ErrEstimatedRoundNotInAllowedSpread,
		},
		{
			name:         "lowest round in the allowed past spread is accepted",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: netCfg.SlotDuration/3 + roundtimer.QuickTimeout*5,
			round:        specqbft.FirstRound + 3,
		},
		{
			name:         "proposer still allows first round when slot-start estimate advanced",
			role:         spectypes.RoleProposer,
			timeIntoSlot: roundtimer.QuickTimeout * 5,
			round:        specqbft.FirstRound,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			signedSSVMessage.SSVMessage.MsgID = spectypes.NewMsgID(netCfg.DomainType, make([]byte, 48), tc.role)
			err := mv.roundBelongsToAllowedSpread(
				signedSSVMessage,
				&specqbft.Message{
					Height: specqbft.Height(slot),
					Round:  tc.round,
				},
				netCfg.SlotStartTime(slot).Add(tc.timeIntoSlot),
			)

			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
