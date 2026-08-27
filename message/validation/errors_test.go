package validation

import (
	"context"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestHandleValidationError_SelfDiscardLeveling pins the logging contract for validation discards:
// our own outbound messages (peerID == selfPID) are leveled by what the discard says about this
// node — ignores and cancellations at debug (routine dedup, shutdown), rejects and timeouts at
// warn (own message refused by own validation, or never published because validation was too
// slow) — while messages received from peers keep their existing debug logging.
func TestHandleValidationError_SelfDiscardLeveling(t *testing.T) {
	const self = peer.ID("self")
	const other = peer.ID("other")

	tests := []struct {
		name       string
		selfPID    peer.ID
		peerID     peer.ID
		err        error
		wantResult pubsub.ValidationResult
		wantLevel  zapcore.Level
		wantMsg    string
	}{
		{
			name:       "own ignored message is quiet (debug)",
			selfPID:    self,
			peerID:     self,
			err:        ErrDecidedMessageWithTooFewSigners, // non-reject -> ignore
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "own outbound message ignored by local validation",
		},
		{
			name:       "own rejected message is surfaced (warn)",
			selfPID:    self,
			peerID:     self,
			err:        ErrEmptyData, // reject:true -> reject
			wantResult: pubsub.ValidationReject,
			wantLevel:  zapcore.WarnLevel,
			wantMsg:    "own outbound message rejected by local validation",
		},
		{
			name:       "own ignore via context cancellation is quiet (debug)",
			selfPID:    self,
			peerID:     self,
			err:        context.Canceled,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "own outbound message ignored by local validation",
		},
		{
			name:       "own drop via validation timeout is surfaced (warn)",
			selfPID:    self,
			peerID:     self,
			err:        context.DeadlineExceeded,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.WarnLevel,
			wantMsg:    "own outbound message dropped by local validation timeout",
		},
		{
			name:       "inbound timeout keeps existing debug log",
			selfPID:    self,
			peerID:     other,
			err:        context.DeadlineExceeded,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "ignoring message due to validation timeout",
		},
		{
			name:       "inbound ignore keeps existing debug log",
			selfPID:    self,
			peerID:     other,
			err:        ErrDecidedMessageWithTooFewSigners,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "ignoring invalid message",
		},
		{
			name:       "inbound reject stays debug (not our bug)",
			selfPID:    self,
			peerID:     other,
			err:        ErrEmptyData,
			wantResult: pubsub.ValidationReject,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "rejecting invalid message",
		},
		{
			name:       "unset selfPID falls back to inbound handling",
			selfPID:    "",
			peerID:     self,
			err:        ErrDecidedMessageWithTooFewSigners,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "ignoring invalid message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			mv := &messageValidator{
				logger:  zap.New(core),
				selfPID: tt.selfPID,
			}

			got := mv.handleValidationError(context.Background(), tt.peerID, nil, tt.err)
			require.Equal(t, tt.wantResult, got)

			entries := logs.All()
			require.Len(t, entries, 1, "expected exactly one log entry")
			require.Equal(t, tt.wantLevel, entries[0].Level)
			require.Equal(t, tt.wantMsg, entries[0].Message)
		})
	}
}
