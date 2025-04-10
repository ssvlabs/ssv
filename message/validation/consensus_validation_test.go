package validation

import (
	"testing"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

func TestMessageValidator_currentEstimatedRound(t *testing.T) {
	tt := []struct {
		name           string
		sinceSlotStart time.Duration
		want           specqbft.Round
	}{
		{
			name:           "0s - expected first round",
			sinceSlotStart: 0,
			want:           specqbft.FirstRound,
		},
		{
			name:           "QuickTimeout/2 - expected first round",
			sinceSlotStart: roundtimer.QuickTimeout / 2,
			want:           specqbft.FirstRound,
		},
		{
			name:           "QuickTimeout - expected first+1 round",
			sinceSlotStart: roundtimer.QuickTimeout,
			want:           specqbft.FirstRound + 1,
		},
		{
			name:           "QuickTimeout*2 - expected first+2 round",
			sinceSlotStart: roundtimer.QuickTimeout * 2,
			want:           specqbft.FirstRound + 2,
		},
		{
			name:           "QuickTimeout*3 - expected first+3 round",
			sinceSlotStart: roundtimer.QuickTimeout * 3,
			want:           specqbft.FirstRound + 3,
		},
		{
			name:           "QuickTimeout*4 - expected first+4 round",
			sinceSlotStart: roundtimer.QuickTimeout * 4,
			want:           specqbft.FirstRound + 4,
		},
		{
			name:           "QuickTimeout*5 - expected first+5 round",
			sinceSlotStart: roundtimer.QuickTimeout * 5,
			want:           specqbft.FirstRound + 5,
		},
		{
			name:           "QuickTimeout*6 - expected first+6 round",
			sinceSlotStart: roundtimer.QuickTimeout * 6,
			want:           specqbft.FirstRound + 6,
		},
		{
			name:           "QuickTimeout*7 - expected first+7 round",
			sinceSlotStart: roundtimer.QuickTimeout * 7,
			want:           specqbft.FirstRound + 7,
		},
		{
			name:           "QuickTimeout*8 - expected first+8 round",
			sinceSlotStart: roundtimer.QuickTimeout * 8,
			want:           specqbft.FirstRound + 8,
		},
		{
			name:           "QuickTimeout*9 - expected first+8 round",
			sinceSlotStart: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+1),
			want:           roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:           "QuickTimeout*10 - expected first+8 round",
			sinceSlotStart: roundtimer.QuickTimeout * time.Duration(roundtimer.QuickTimeoutThreshold+2),
			want:           roundtimer.QuickTimeoutThreshold + 1,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout) - expected first+9 round",
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout,
			want:           roundtimer.QuickTimeoutThreshold + 2,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout*2) - expected first+10 round",
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*2,
			want:           roundtimer.QuickTimeoutThreshold + 3,
		},
		{
			name:           "(QuickTimeout*8 + SlowTimeout*3) - expected first+11 round",
			sinceSlotStart: roundtimer.QuickTimeout*time.Duration(roundtimer.QuickTimeoutThreshold) + roundtimer.SlowTimeout*3,
			want:           roundtimer.QuickTimeoutThreshold + 4,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mv := &messageValidator{}
			got, err := mv.currentEstimatedRound(tc.sinceSlotStart)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
