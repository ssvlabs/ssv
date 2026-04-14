package operator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/v2/exporter"
)

func TestShouldRunDutyScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		exporterOpts exporter.Options
		expected     bool
	}{
		{
			name:         "regular operator",
			exporterOpts: exporter.Options{},
			expected:     true,
		},
		{
			name: "exporter standard",
			exporterOpts: exporter.Options{
				Enabled: true,
				Mode:    exporter.ModeStandard,
			},
			expected: false,
		},
		{
			name: "exporter archive",
			exporterOpts: exporter.Options{
				Enabled: true,
				Mode:    exporter.ModeArchive,
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, shouldRunDutyScheduler(tc.exporterOpts))
		})
	}
}
