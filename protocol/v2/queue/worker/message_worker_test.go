package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestWorker(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 1,
		Buffer:       2,
	})

	handlerErrCh := make(chan error, 1)
	processed := make(chan struct{}, 1)
	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		processed <- struct{}{}
		return nil
	})

	for i := 0; i < 5; i++ {
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
		select {
		case <-processed:
		case <-testCtx.Done():
			t.Fatalf("timed out waiting for message %d to be processed", i)
		}
	}
	assertNoAsyncError(t, handlerErrCh)
}

func TestManyWorkers(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 10,
		Buffer:       10,
	})

	handlerErrCh := make(chan error, 1)
	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		wg.Done()
		return nil
	})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for workers to process messages")
	}
	assertNoAsyncError(t, handlerErrCh)
}

func TestBuffer(t *testing.T) {
	testCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	logger := log.TestLogger(t)
	var wg sync.WaitGroup

	worker := NewWorker(logger, &Config{
		Ctx:          testCtx,
		WorkersCount: 1,
		Buffer:       10,
	})

	const totalMessages = 11
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	wg.Add(totalMessages)
	handlerErrCh := make(chan error, 1)

	worker.UseHandler(func(ctx context.Context, msg network.DecodedSSVMessage) error {
		if msg == nil {
			select {
			case handlerErrCh <- errors.New("received nil message"):
			default:
			}
			return nil
		}
		once.Do(func() {
			close(started)
		})
		<-release
		wg.Done()
		return nil
	})

	// Let one message start processing, then fill the queue buffer.
	require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	select {
	case <-started:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for handler to start")
	}

	for i := 0; i < totalMessages-1; i++ { // should fill the 10-sized buffer
		require.True(t, worker.TryEnqueue(&queue.SSVMessage{}))
	}
	require.False(t, worker.TryEnqueue(&queue.SSVMessage{}), "queue should be full")

	close(release)

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
	case <-testCtx.Done():
		t.Fatal("timed out waiting for buffered messages to be processed")
	}
	assertNoAsyncError(t, handlerErrCh)
}

func TestMessageContextFields(t *testing.T) {
	t.Run("nil message", func(t *testing.T) {
		require.Nil(t, messageContextFields(nil))
	})

	t.Run("committee message includes slot and committee id", func(t *testing.T) {
		msgID := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)
		fields := messageContextFields(&queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgID:   msgID,
				MsgType: spectypes.SSVPartialSignatureMsgType,
			},
			Body: &spectypes.PartialSignatureMessages{Slot: 9},
		})

		require.Contains(t, fieldKeys(fields), "msg_id")
		require.Contains(t, fieldKeys(fields), "msg_type")
		require.Contains(t, fieldKeys(fields), "runner_role")
		require.Contains(t, fieldKeys(fields), "slot")
		require.Contains(t, fieldKeys(fields), "committee_id")
	})

	t.Run("validator message omits slot and committee id when slot unavailable", func(t *testing.T) {
		msgID := spectypes.NewMsgID([4]byte{}, []byte("validator_pk"), ssvtypes.RoleAggregator)
		fields := messageContextFields(&queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgID:   msgID,
				MsgType: spectypes.SSVPartialSignatureMsgType,
			},
		})

		require.Contains(t, fieldKeys(fields), "msg_id")
		require.Contains(t, fieldKeys(fields), "msg_type")
		require.Contains(t, fieldKeys(fields), "runner_role")
		require.NotContains(t, fieldKeys(fields), "slot")
		require.NotContains(t, fieldKeys(fields), "committee_id")
	})
}

func TestWorkerProcess_LogsMessageContextOnError(t *testing.T) {
	core, recorded := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	msgID := spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee)

	worker := &Worker{
		handler: func(context.Context, network.DecodedSSVMessage) error {
			return errors.New("handler boom")
		},
		errHandler: func(msg *queue.SSVMessage, err error) error {
			require.EqualError(t, err, "handler boom")
			return errors.New("wrapped boom")
		},
	}

	msg := &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgID:   msgID,
			MsgType: spectypes.SSVPartialSignatureMsgType,
		},
		Body: &spectypes.PartialSignatureMessages{Slot: phase0.Slot(13)},
	}

	worker.process(t.Context(), logger, msg)

	logs := recorded.FilterMessage("❌ failed to handle message").All()
	require.Len(t, logs, 1)

	fields := logs[0].ContextMap()
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], msgID.GetDutyExecutorID()[16:])

	require.Equal(t, msgID.String(), fields["msg_id"])
	require.Equal(t, "partial_signature", fields["msg_type"])
	require.Contains(t, fields, "runner_role")
	require.EqualValues(t, 13, fields["slot"])
	require.EqualValues(t, "wrapped boom", fields["error"])
	require.EqualValues(t, fmt.Sprintf("%x", committeeID), fields["committee_id"])
}

func assertNoAsyncError(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func fieldKeys(fields []zap.Field) map[string]struct{} {
	keys := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		keys[field.Key] = struct{}{}
	}
	return keys
}
