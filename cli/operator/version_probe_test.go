package operator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionProbeMode(t *testing.T) {
	tests := []struct {
		name       string
		isExporter bool
		flag       bool
		wantEnable bool
		wantWarn   string
	}{
		{"exporter, flag on", true, true, true, ""},
		{"exporter, flag off", true, false, false, ""},
		{"non-exporter, flag on", false, true, false, "EXPORTER_PROBE_PEER_VERSIONS is set but the node is not in exporter mode; ignoring"},
		{"non-exporter, flag off", false, false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enable, warn := versionProbeMode(tc.isExporter, tc.flag)
			require.Equal(t, tc.wantEnable, enable)
			require.Equal(t, tc.wantWarn, warn)
		})
	}
}
