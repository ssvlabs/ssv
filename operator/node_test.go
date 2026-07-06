package operator

import (
	"context"
	"testing"

	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	exporterconfig "github.com/ssvlabs/ssv/exporter/config"
	"github.com/ssvlabs/ssv/exporter/v1/api"
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
	handler api.QueryMessageHandler
	started bool
	feed    *event.Feed
}

func (s *recordingWebSocketServer) Start(context.Context) (string, <-chan error, error) {
	s.started = true
	serveErr := make(chan error)
	close(serveErr)
	return "", serveErr, nil
}

func (s *recordingWebSocketServer) BroadcastFeed() *event.Feed {
	if s.feed == nil {
		s.feed = new(event.Feed)
	}
	return s.feed
}

func (s *recordingWebSocketServer) UseQueryHandler(handler api.QueryMessageHandler) {
	s.handler = handler
}

func TestStartWSServerUsesInjectedQueryHandler(t *testing.T) {
	ws := &recordingWebSocketServer{}
	called := false
	queryHandler := func(*api.NetworkMessage) {
		called = true
	}

	n := &Node{
		logger:         zap.NewNop(),
		ws:             ws,
		wsQueryHandler: queryHandler,
	}

	serveErr, err := n.startWSServer(context.Background())
	require.NoError(t, err)
	require.NotNil(t, serveErr)
	require.True(t, ws.started)
	require.NotNil(t, ws.handler)

	ws.handler(&api.NetworkMessage{})
	require.True(t, called)
}
