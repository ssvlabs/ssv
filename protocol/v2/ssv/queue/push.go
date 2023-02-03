package queue

import "time"

// Pusher is a strategy for pushing new messages to the queue.
type Pusher func(chan *DecodedSSVMessage, *DecodedSSVMessage)

// PusherDropping drops the oldest pending message when the queue is full.
func PusherDropping(patience time.Duration) Pusher {
	return func(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
		// Try pushing without blocking.
		select {
		case inbox <- msg:
			// Pushed successfully.
			return
		default:
			// Inbox is at capacity.
		}

		// Try pushing again with a timeout.
		if patience > 0 {
			select {
			case inbox <- msg:
				// Pushed successfully.
				return
			case <-time.After(patience):
				// Inbox is still at capacity.
			}
		}

		// Drop the oldest pending message and push the new one.
		select {
		case <-inbox:
			metricMessageDropped.WithLabelValues("queue").Inc()
		default:
		}
		inbox <- msg
	}
}

// PusherBlocking blocks when the queue is full.
func PusherBlocking() Pusher {
	return func(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
		inbox <- msg
	}
}
