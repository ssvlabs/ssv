package queue

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type ChannelUpdater struct {
	queue       Queue
	messageChan chan *DecodedSSVMessage
	logger      *zap.Logger
	prioritizer MessagePrioritizer
	filter      Filter
	lock        sync.Mutex
}

func NewChannelUpdater(l *zap.Logger, q Queue, prioritizer MessagePrioritizer, filter Filter) *ChannelUpdater {
	return &ChannelUpdater{
		queue:       q,
		messageChan: make(chan *DecodedSSVMessage),
		logger:      l,
		prioritizer: prioritizer,
		filter:      filter,
	}
}

func (cu *ChannelUpdater) Start(ctx context.Context) {
	go cu.updateChannel(ctx)
}

func (cu *ChannelUpdater) GetChannel() chan *DecodedSSVMessage {
	return cu.messageChan
}

func (cu *ChannelUpdater) UpdatePrioritizer(p MessagePrioritizer) {
	cu.lock.Lock()
	defer cu.lock.Unlock()
	cu.prioritizer = p
}

func (cu *ChannelUpdater) UpdateFilter(f Filter) {
	cu.lock.Lock()
	defer cu.lock.Unlock()
	cu.filter = f
}

// Update both prioritizer and filter in one go
func (cu *ChannelUpdater) UpdatePrioritizerAndFilter(p MessagePrioritizer, f Filter) {
	cu.lock.Lock()
	defer cu.lock.Unlock()
	cu.prioritizer = p
	cu.filter = f
}

func (cu *ChannelUpdater) updateChannel(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			close(cu.messageChan)
			return
		default:
			var currentPrioritizer MessagePrioritizer
			var currentFilter Filter

			cu.lock.Lock()
			currentPrioritizer = cu.prioritizer
			currentFilter = cu.filter
			cu.lock.Unlock()

			msg := cu.queue.Pop(ctx, currentPrioritizer, currentFilter)

			if ctx.Err() != nil {
				close(cu.messageChan)
				return
			}

			if msg == nil {
				close(cu.messageChan)
				return
			}

			cu.messageChan <- msg
		}
	}
}
