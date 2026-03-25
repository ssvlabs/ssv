package streams

import (
	"bytes"
	"context"
	"testing"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

func TestStreamCtrl(t *testing.T) {
	hosts := testHosts(t, 3)

	prot := protocol.ID("/test/protocol")

	ctrl0 := NewStreamController(t.Context(), hosts[0], time.Second, time.Second)
	ctrl1 := NewStreamController(t.Context(), hosts[1], time.Second, time.Second)

	t.Run("handle request", func(t *testing.T) {
		logger := zap.NewNop()
		hosts[0].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, res, done, err := ctrl0.HandleStream(logger, stream)
			defer done()
			require.NoError(t, err)
			require.NotNil(t, msg)
			resp, err := dummyMsg().Encode()
			require.NoError(t, err)
			require.NoError(t, res(resp))
		})
		d, err := dummyMsg().Encode()
		require.NoError(t, err)
		res, err := ctrl1.Request(logger, hosts[0].ID(), prot, d)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, bytes.Equal(res, d))
	})

	t.Run("request deadline", func(t *testing.T) {
		logger := zap.NewNop()
		timeout := time.Millisecond * 10
		ctrl0.(*streamCtrl).readWriteTimeout = timeout
		hosts[1].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			msg, s, done, err := ctrl0.HandleStream(logger, stream)
			done()
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.NotNil(t, s)
			<-time.After(timeout + time.Millisecond)
		})
		d, err := dummyMsg().Encode()
		require.NoError(t, err)
		res, err := ctrl0.Request(logger, hosts[0].ID(), prot, d)
		require.Error(t, err)
		require.Nil(t, res)
	})

	t.Run("reject oversized response", func(t *testing.T) {
		core, observed := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)
		ctrl0.(*streamCtrl).readWriteTimeout = time.Second
		oversized := bytes.Repeat([]byte("x"), maxStreamMessageSize+1)

		hosts[1].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			s := NewStream(stream)
			defer s.Close()
			require.NoError(t, s.WriteWithTimeout(oversized, time.Second))
		})

		d, err := dummyMsg().Encode()
		require.NoError(t, err)

		res, err := ctrl0.Request(logger, hosts[1].ID(), prot, d)
		require.ErrorIs(t, err, ErrStreamMessageTooLarge)
		require.Nil(t, res)
		requireObservedOversizedLog(t, observed, hosts[1].ID(), prot, "response")
	})

	t.Run("reject oversized request", func(t *testing.T) {
		core, observed := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)
		ctrl0.(*streamCtrl).readWriteTimeout = time.Second
		oversized := bytes.Repeat([]byte("x"), maxStreamMessageSize+1)
		handlerDone := make(chan error, 1)

		hosts[0].SetStreamHandler(prot, func(stream libp2pnetwork.Stream) {
			_, _, done, err := ctrl0.HandleStream(logger, stream)
			defer done()
			handlerDone <- err
		})

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		s, err := hosts[1].NewStream(ctx, hosts[0].ID(), prot)
		require.NoError(t, err)
		defer s.Close()
		require.NoError(t, s.SetWriteDeadline(time.Now().Add(time.Second)))
		_, _ = s.Write(oversized)

		require.ErrorIs(t, <-handlerDone, ErrStreamMessageTooLarge)
		requireObservedOversizedLog(t, observed, hosts[1].ID(), prot, "request")
	})
}

func dummyMsg() *spectypes.SSVMessage {
	return &spectypes.SSVMessage{Data: []byte("dummy")}
}

func requireObservedOversizedLog(t *testing.T, logs *observer.ObservedLogs, peerID peer.ID, prot protocol.ID, direction string) {
	t.Helper()

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.WarnLevel, entries[0].Level)
	require.Equal(t, "rejected oversized stream payload", entries[0].Message)

	fieldsMap := entries[0].ContextMap()
	require.Equal(t, peerID.String(), fieldsMap[fields.FieldPeerID])
	require.Equal(t, string(prot), fieldsMap[fields.FieldProtocolID])
	require.Equal(t, direction, fieldsMap["direction"])
}
