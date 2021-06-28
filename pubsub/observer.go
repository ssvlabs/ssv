package pubsub

import (
	"sync"
)

const defaultChannelBuffer = 10

func newChannel(bufSize int) SubjectChannel {
	return make(SubjectChannel, bufSize)
}

// observer is an internal abstraction on top of channels
type observer struct {
	channel SubjectChannel
	active  bool
	mut     sync.Mutex
}

func newSubjectObserver() *observer {
	so := observer{
		newChannel(defaultChannelBuffer),
		true,
		sync.Mutex{},
	}
	return &so
}

// isActive is a race-free way of checking observer activity
func (so *observer) isActive() bool {
	so.mut.Lock()
	defer so.mut.Unlock()

	return so.active
}

func (so *observer) close() {
	so.mut.Lock()
	defer so.mut.Unlock()

	if so.active {
		so.active = false
		close(so.channel)
	}
}

func (so *observer) notifyCallback(e SubjectEvent) {
	if so.isActive() {
		go func() {
			// in case the channel is blocking - the observer should be locked
			// and therefore the lock is acquired again
			so.mut.Lock()
			defer so.mut.Unlock()

			so.channel <- e
		}()
	}
}
