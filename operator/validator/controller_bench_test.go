package validator

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/queue/worker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	validatorprotocol "github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

const benchmarkWorkerCount = 256

func BenchmarkRouterFanout(b *testing.B) {
	for _, fanout := range []int{16, 32, 40, 48, 56, 64, 80, 96, 128, 256, 2048} {
		b.Run(fmt.Sprintf("fanout=%d", fanout), func(b *testing.B) {
			ctrl, cancel, state := newBenchmarkController(b, fanout)
			defer cancel()

			for i := 0; i < fanout; i++ {
				go ctrl.handleRouterMessages()
			}

			msg := benchmarkRouterMessage()

			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					ctrl.messageRouter.Route(ctrl.ctx, msg)
				}
			})

			elapsed := time.Since(start)
			b.StopTimer()

			require.Eventually(b, func() bool {
				return len(ctrl.messageRouter.ch) == 0 && ctrl.messageWorker.Size() == 0 && state.inFlight.Load() == 0
			}, 10*time.Second, 10*time.Millisecond)

			b.ReportMetric(float64(state.processed.Load())/elapsed.Seconds(), "msgs/s")
		})
	}
}

type benchmarkState struct {
	processed atomic.Int64
	inFlight  atomic.Int64
}

func newBenchmarkController(b *testing.B, fanout int) (*Controller, context.CancelFunc, *benchmarkState) {
	b.Helper()

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	state := &benchmarkState{}

	ctrl := &Controller{
		logger:        logger,
		ctx:           ctx,
		networkConfig: networkconfig.TestNetwork,
		validatorsMap: validators.New(ctx),
		messageRouter: newMessageRouter(logger),
		messageWorker: worker.NewWorker(logger, &worker.Config{
			Ctx:          ctx,
			WorkersCount: benchmarkWorkerCount,
			Buffer:       1 << 20,
		}),
		validatorCommonOpts: &validatorprotocol.CommonOptions{
			ExporterOptions: exporter.Options{Enabled: true},
		},
	}

	ctrl.messageWorker.UseHandler(func(context.Context, network.DecodedSSVMessage) error {
		state.inFlight.Add(1)
		defer state.inFlight.Add(-1)
		state.processed.Add(1)
		return nil
	})

	return ctrl, cancel, state
}

func benchmarkRouterMessage() *queue.SSVMessage {
	return &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, []byte("router-benchmark-message"), spectypes.RoleCommittee),
			Data:    []byte("bench"),
		},
	}
}
