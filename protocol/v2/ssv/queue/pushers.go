package queue

import (
	"sync/atomic"
	"time"
)

// Pusher is a strategy for pushing new messages to the queue.
type Pusher func(chan *DecodedSSVMessage, *DecodedSSVMessage)

// PusherBlocking blocks when the queue is full.
func PusherBlocking() Pusher {
	return func(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
		inbox <- msg
	}
}

// PusherDropping drops the oldest pending message when the queue is full.
// If patience > 0, it tries pushing with a timeout before dropping.
// Each try decreases the patience until maxTries is reached,
// at which point patience is ignored and oldest messages are
// dropped immediately until the queue is no longer full.
func PusherDropping(patience time.Duration, maxTries int32) Pusher {
	var (
		tries     atomic.Int32
		decayRate = time.Duration(1 / float64(maxTries) * float64(patience))
	)
	return func(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
		// Try pushing immediately.
		select {
		case inbox <- msg:
			// Pushed successfully.
			if patience > 0 {
				tries.Store(0)
			}
			return
		default:
			// Inbox is at capacity.
		}

		// Try pushing again with a timeout.
		if patience > 0 {
			if try := tries.Load(); try < maxTries {
				tries.Add(1)

				select {
				case inbox <- msg:
					// Pushed successfully.
					return
				case <-time.After(patience - time.Duration(try)*decayRate):
					// Inbox is still at capacity.
				}
			}
		}

		// Drop the oldest pending message and push the new one.
		for {
			select {
			case <-inbox:
				metricMessageDropped.WithLabelValues(msg.MsgID.String()).Inc()
			default:
			}
			select {
			case inbox <- msg:
				return
			default:
			}
		}
	}
}
