package operator

import (
	"testing"

	"github.com/stretchr/testify/require"

	exporterconfig "github.com/ssvlabs/ssv/exporter/config"
)

func TestShouldRunDutyScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		exporterOpts exporterconfig.Options
		expected     bool
	}{
		{
			name:         "regular operator",
			exporterOpts: exporterconfig.Options{},
			expected:     true,
		},
		{
			name: "exporter standard",
			exporterOpts: exporterconfig.Options{
				Enabled: true,
				Mode:    exporterconfig.ModeStandard,
			},
			expected: false,
		},
		{
			name: "exporter archive",
			exporterOpts: exporterconfig.Options{
				Enabled: true,
				Mode:    exporterconfig.ModeArchive,
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
