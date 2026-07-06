package operator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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

type recordingWebSocketServer struct {
	started bool
}

func (s *recordingWebSocketServer) Start(context.Context) (string, <-chan error, error) {
	s.started = true
	serveErr := make(chan error)
	close(serveErr)
	return "", serveErr, nil
}

func TestStartWSServerStartsConfiguredServer(t *testing.T) {
	ws := &recordingWebSocketServer{}

	n := &Node{
		logger: zap.NewNop(),
		ws:     ws,
	}

	serveErr, err := n.startWSServer(context.Background())
	require.NoError(t, err)
	require.NotNil(t, serveErr)
	require.True(t, ws.started)
}
