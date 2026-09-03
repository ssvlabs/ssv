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
// node — a routine self-ignore (own dedup / benign slot-timing race, see routineSelfIgnores) and a
// cancellation (shutdown) at debug, while a reject, a timeout, or any non-routine ignore is at warn
// (own message refused by own validation, or never published because validation was too slow) —
// while messages received from peers keep their existing debug logging. It also pins that the topic
// threaded in by the caller is logged even when the message never decoded (nil decodedMessage).
func TestHandleValidationError_SelfDiscardLeveling(t *testing.T) {
	const self = peer.ID("self")
	const other = peer.ID("other")

	tests := []struct {
		name       string
		selfPID    peer.ID
		peerID     peer.ID
		topic      string
		err        error
		wantResult pubsub.ValidationResult
		wantLevel  zapcore.Level
		wantMsg    string
	}{
		{
			name:       "own routine ignore is quiet (debug)",
			selfPID:    self,
			peerID:     self,
			err:        ErrDecidedMessageWithTooFewSigners, // non-reject, routine -> ignore/debug
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "own outbound message ignored by local validation",
		},
		{
			name:       "own non-routine ignore is surfaced (warn)",
			selfPID:    self,
			peerID:     self,
			err:        ErrIncorrectTopic, // non-reject but not routine -> ignore/warn
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.WarnLevel,
			wantMsg:    "own outbound message ignored by local validation (unexpected)",
		},
		{
			name:       "own rejected message is surfaced (warn) with topic",
			selfPID:    self,
			peerID:     self,
			topic:      "ssv.v2.some-topic", // pre-decode reject -> nil decodedMessage; topic is all we have
			err:        ErrEmptyData,        // reject:true -> reject
			wantResult: pubsub.ValidationReject,
			wantLevel:  zapcore.WarnLevel,
			wantMsg:    "own outbound message rejected by local validation",
		},
		{
			name:       "own cancellation is quiet (debug)",
			selfPID:    self,
			peerID:     self,
			err:        context.Canceled,
			wantResult: pubsub.ValidationIgnore,
			wantLevel:  zapcore.DebugLevel,
			wantMsg:    "own outbound message discarded by local validation cancellation",
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
			err:        ErrIncorrectTopic, // non-routine for self, but inbound stays debug
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

			got := mv.handleValidationError(context.Background(), tt.peerID, tt.topic, nil, tt.err)
			require.Equal(t, tt.wantResult, got)

			entries := logs.All()
			require.Len(t, entries, 1, "expected exactly one log entry")
			require.Equal(t, tt.wantLevel, entries[0].Level)
			require.Equal(t, tt.wantMsg, entries[0].Message)
			if tt.topic != "" {
				require.Equal(t, tt.topic, entries[0].ContextMap()["topic"],
					"topic threaded by the caller must be logged even with nil decodedMessage")
			}
		})
	}
}
