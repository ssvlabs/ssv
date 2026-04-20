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
			ctrl, cancel, processed := newBenchmarkController(b, fanout)
			defer cancel()

			for i := 0; i < fanout; i++ {
				go ctrl.handleRouterMessages()
			}

			msg := benchmarkRouterMessage()
			var sent atomic.Int64

			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					sent.Add(1)
					ctrl.messageRouter.ch <- msg
				}
			})

			target := sent.Load()
			require.Eventually(b, func() bool {
				return processed.Load() == target
			}, 10*time.Second, 10*time.Millisecond)

			elapsed := time.Since(start)
			b.StopTimer()
			b.ReportMetric(float64(target)/elapsed.Seconds(), "msgs/s")
		})
	}
}

func newBenchmarkController(b *testing.B, fanout int) (*Controller, context.CancelFunc, *atomic.Int64) {
	b.Helper()

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	processed := &atomic.Int64{}

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
		processed.Add(1)
		return nil
	})

	return ctrl, cancel, processed
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
