package validator

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	validatorprotocol "github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

const (
	benchmarkCommitteeCount = 4
	benchmarkMaxOutstanding = 1024
)

// BenchmarkRouterFanout keeps a fixed amount of work in flight so the router
// stays in steady state instead of spending the whole run saturated.
func BenchmarkRouterFanout(b *testing.B) {
	for _, fanout := range []int{8, 12, 16, 24, 32, 40, 48, 56, 64, 80, 96, 128, 256, 2048} {
		b.Run(fmt.Sprintf("fanout=%d", fanout), func(b *testing.B) {
			ctrl, cancel, state := newBenchmarkController(b)
			defer cancel()

			for i := 0; i < fanout; i++ {
				go ctrl.handleRouterMessages()
			}

			producerCount := runtime.GOMAXPROCS(0)
			var producers sync.WaitGroup
			producers.Add(producerCount)

			b.ReportAllocs()
			// Use the reported *_msgs/s metrics for throughput; ns/op includes the drain wait below.
			b.ResetTimer()
			start := time.Now()

			for producerID := 0; producerID < producerCount; producerID++ {
				go func() {
					defer producers.Done()

					for {
						sequence := state.nextSequence.Add(1) - 1
						if sequence >= int64(b.N) {
							return
						}

						<-state.credits

						msg := state.messageFor(sequence)
						for {
							state.attempted.Add(1)
							if ctrl.messageRouter.route(ctrl.ctx, msg) {
								state.routerEnqueued.Add(1)
								state.observeOutstanding(state.outstanding.Add(1))
								break
							}

							state.routerDropped.Add(1)
							runtime.Gosched()
						}
					}
				}()
			}

			producers.Wait()
			require.Eventually(b, func() bool {
				return state.processed.Load() == int64(b.N)
			}, 30*time.Second, time.Millisecond)

			elapsed := time.Since(start)
			b.StopTimer()

			routerEnqueued := state.routerEnqueued.Load()
			processed := state.processed.Load()
			routerBacklog := int64(ctrl.messageRouter.Len())
			committeeBacklog := state.committeeBacklog()

			b.ReportMetric(float64(routerEnqueued)/elapsed.Seconds(), "router_msgs/s")
			b.ReportMetric(float64(processed)/elapsed.Seconds(), "processed_msgs/s")
			if attempted := state.attempted.Load(); attempted > 0 {
				b.ReportMetric(float64(state.routerDropped.Load())/float64(attempted)*100, "router_drop_pct")
				b.ReportMetric(float64(attempted)/float64(processed), "attempts/msg")
			}
			b.ReportMetric(float64(routerBacklog), "router_backlog")
			b.ReportMetric(float64(committeeBacklog), "committee_backlog")
			b.ReportMetric(float64(state.maxOutstanding.Load()), "max_outstanding")
		})
	}
}

type benchmarkState struct {
	attempted      atomic.Int64
	nextSequence   atomic.Int64
	routerEnqueued atomic.Int64
	routerDropped  atomic.Int64
	processed      atomic.Int64
	outstanding    atomic.Int64
	maxOutstanding atomic.Int64
	credits        chan struct{}
	queues         []queue.Queue
	messages       []*queue.SSVMessage
}

func newBenchmarkController(b *testing.B) (*Controller, context.CancelFunc, *benchmarkState) {
	b.Helper()

	logger := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	state := &benchmarkState{
		credits:  make(chan struct{}, benchmarkMaxOutstanding),
		queues:   make([]queue.Queue, 0, benchmarkCommitteeCount),
		messages: make([]*queue.SSVMessage, 0, benchmarkCommitteeCount),
	}
	for i := 0; i < benchmarkMaxOutstanding; i++ {
		state.credits <- struct{}{}
	}

	validatorsMap := validators.New(ctx)
	for committeeIndex := 0; committeeIndex < benchmarkCommitteeCount; committeeIndex++ {
		msg, committeeQueue := benchmarkCommitteeFixture(ctx, logger, validatorsMap, committeeIndex)
		state.messages = append(state.messages, msg)
		state.queues = append(state.queues, committeeQueue)
		go benchmarkConsumeCommitteeQueue(ctx, state, committeeQueue, phase0.Slot(committeeIndex+1))
	}

	ctrl := &Controller{
		logger:        logger,
		ctx:           ctx,
		networkConfig: networkconfig.TestNetwork,
		validatorsMap: validatorsMap,
		messageRouter: newMessageRouter(logger),
	}

	return ctrl, cancel, state
}

func benchmarkCommitteeFixture(
	ctx context.Context,
	logger *zap.Logger,
	validatorsMap *validators.ValidatorsMap,
	committeeIndex int,
) (*queue.SSVMessage, queue.Queue) {
	committeeMembers := []*spectypes.Operator{
		{OperatorID: spectypes.OperatorID(committeeIndex*10 + 1)},
		{OperatorID: spectypes.OperatorID(committeeIndex*10 + 2)},
		{OperatorID: spectypes.OperatorID(committeeIndex*10 + 3)},
		{OperatorID: spectypes.OperatorID(committeeIndex*10 + 4)},
	}
	committeeID := spectypes.GetCommitteeID([]spectypes.OperatorID{
		committeeMembers[0].OperatorID,
		committeeMembers[1].OperatorID,
		committeeMembers[2].OperatorID,
		committeeMembers[3].OperatorID,
	})
	slot := phase0.Slot(committeeIndex + 1)

	committee := validatorprotocol.NewCommittee(
		logger,
		networkconfig.TestNetwork,
		&spectypes.CommitteeMember{
			CommitteeID: committeeID,
			Committee:   committeeMembers,
			FaultyNodes: 1,
		},
		nil,
		nil,
		nil,
	)
	validatorsMap.PutCommittee(committeeID, committee)

	warmupMsg := benchmarkRouterMessage(committeeID, slot)
	committee.EnqueueMessage(ctx, warmupMsg)
	committeeQueue := committee.Queues[slot].Q
	committeeQueue.TryPop(queue.NewCommitteeQueuePrioritizer(&queue.State{
		Slot:   slot,
		Quorum: 3,
	}), queue.FilterAny)

	return warmupMsg, committeeQueue
}

func benchmarkRouterMessage(committeeID spectypes.CommitteeID, slot phase0.Slot) *queue.SSVMessage {
	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, committeeID[:], spectypes.RoleCommittee)

	qbftMsg := &specqbft.Message{
		Height:     specqbft.Height(slot),
		Round:      1,
		MsgType:    specqbft.PrepareMsgType,
		Identifier: msgID[:],
	}
	data, err := qbftMsg.Encode()
	if err != nil {
		panic(err)
	}

	return &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   msgID,
			Data:    data,
		},
		Body: qbftMsg,
	}
}

func benchmarkConsumeCommitteeQueue(ctx context.Context, state *benchmarkState, q queue.Queue, slot phase0.Slot) {
	prioritizer := queue.NewCommitteeQueuePrioritizer(&queue.State{
		Slot:   slot,
		Quorum: 3,
	})
	for {
		msg := q.TryPop(prioritizer, queue.FilterAny)
		if msg != nil {
			state.processed.Add(1)
			state.outstanding.Add(-1)
			state.credits <- struct{}{}
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
			runtime.Gosched()
		}
	}
}

func (s *benchmarkState) messageFor(sequence int64) *queue.SSVMessage {
	return s.messages[int(sequence%int64(len(s.messages)))]
}

func (s *benchmarkState) committeeBacklog() int64 {
	var backlog int64
	for _, q := range s.queues {
		backlog += int64(q.Len())
	}
	return backlog
}

func (s *benchmarkState) observeOutstanding(outstanding int64) {
	for {
		current := s.maxOutstanding.Load()
		if outstanding <= current {
			return
		}
		if s.maxOutstanding.CompareAndSwap(current, outstanding) {
			return
		}
	}
}
