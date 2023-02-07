package queue

// Pusher is a strategy for pushing new messages to the queue.
type Pusher func(chan *DecodedSSVMessage, *DecodedSSVMessage)

// PusherBlocking blocks until the message is pushed to the queue.
func PusherBlocking(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
	inbox <- msg
}

// PusherDropping tries pushing the message immediately, and if the queue is at capacity,
// it begins dropping the oldest messages until the message is pushed.
func PusherDropping(inbox chan *DecodedSSVMessage, msg *DecodedSSVMessage) {
	// Try pushing immediately.
	select {
	case inbox <- msg:
		// Pushed successfully.
		return
	default:
		// Inbox is at capacity.
	}

	for {
		// Try dropping the oldest message.
		select {
		case <-inbox:
			metricMessageDropped.WithLabelValues(msg.MsgID.String()).Inc()
		default:
		}

		// Try pushing again.
		select {
		case inbox <- msg:
			// Pushed successfully.
			return
		default:
			// Inbox is still at capacity.
		}
	}
}
