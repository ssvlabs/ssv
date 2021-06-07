package pubsub

// observer is an internal abstraction on top of channels
type observer struct {
	channel SubjectChannel
	active  bool
}

func newSubjectObserver() *observer {
	so := observer{
		make(SubjectChannel),
		true,
	}
	return &so
}

func (so *observer) close() {
	so.active = false
}

func (so *observer) notifyCallback(e SubjectEvent) {
	if so.active {
		so.channel <- e
	}
}
