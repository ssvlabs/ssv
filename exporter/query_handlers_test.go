package exporter

import (
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestHandleUnknownQuery(t *testing.T) {
	logger := zap.L()

	nm := api.NetworkMessage{
		Msg: api.Message{
			Type:   "unknown_type",
			Filter: api.MessageFilter{},
		},
		Err:  nil,
		Conn: nil,
	}

	handleUnknownQuery(logger, &nm)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok)
	require.Equal(t, "bad request - unknown message type 'unknown_type'", errs[0])
}

func TestHandleErrorQuery(t *testing.T) {
	logger := zap.L()

	tests := []struct {
		expectedErr string
		netErr      error
		name        string
	}{
		{
			"dummy",
			errors.New("dummy"),
			"network error",
		},
		{
			unknownError,
			nil,
			"unknown error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nm := api.NetworkMessage{
				Msg: api.Message{
					Type:   api.TypeError,
					Filter: api.MessageFilter{},
				},
				Err:  test.netErr,
				Conn: nil,
			}
			handleErrorQuery(logger, &nm)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok)
			require.Equal(t, test.expectedErr, errs[0])
		})
	}
}
