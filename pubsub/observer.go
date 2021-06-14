package pubsub

import "sync"

// observer is an internal abstraction on top of channels
type observer struct {
	channel SubjectChannel
	active  bool
	mut     sync.Mutex
}

func newSubjectObserver() *observer {
	so := observer{
		make(SubjectChannel),
		true,
		sync.Mutex{},
	}
	return &so
}

func (so *observer) close() {
	so.mut.Lock()
	defer so.mut.Unlock()

	so.active = false
}

func (so *observer) notifyCallback(e SubjectEvent) {
	so.mut.Lock()
	defer so.mut.Unlock()

	if so.active {
		go func() {
			so.channel <- e
		}()
	}
}
