package topics

import (
	"context"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"sync"
	"sync/atomic"
)

const (
	publishLimit = int32(256)
)

type topicContainer struct {
	topic  *pubsub.Topic
	sub    *pubsub.Subscription
	locker *sync.Mutex
	// count is the number of subscriptions made for this topic
	subsCount  int32
	activePubs int32
}

func newTopicContainer() *topicContainer {
	return &topicContainer{
		locker: &sync.Mutex{},
	}
}

func (tc *topicContainer) Close() error {
	if tc.sub != nil {
		tc.sub.Cancel()
		tc.sub = nil
	}
	if tc.topic != nil {
		if err := tc.topic.Close(); err != nil {
			return err
		}
		tc.topic = nil
	}
	return nil
}

func (tc *topicContainer) incSubCount() int32 {
	return atomic.AddInt32(&tc.subsCount, 1)
}

func (tc *topicContainer) decSubCount() int32 {
	return atomic.AddInt32(&tc.subsCount, -1)
}

func (tc *topicContainer) Publish(ctx context.Context, data []byte) error {
	if atomic.AddInt32(&tc.activePubs, 1) > publishLimit {
		atomic.AddInt32(&tc.activePubs, -1)
		return errors.New("could not publish msg as outbound q is full")
	}
	go func() {
		topic := tc.name()
		defer atomic.AddInt32(&tc.activePubs, -1)
		err := tc.publish(ctx, data)
		// TODO: log error?
		if err == nil {
			metricPubsubOutbound.WithLabelValues(topic).Inc()
		}
	}()
	return nil
}

func (tc *topicContainer) name() string {
	tc.locker.Lock()
	defer tc.locker.Unlock()

	if tc.topic == nil {
		return ""
	}
	return tc.topic.String()
}

func (tc *topicContainer) publish(ctx context.Context, data []byte) error {
	tc.locker.Lock()
	defer tc.locker.Unlock()

	if tc.topic == nil {
		return ErrTopicNotReady
	}

	return tc.topic.Publish(ctx, data)
}
