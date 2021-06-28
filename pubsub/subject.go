package pubsub

import (
	"github.com/pkg/errors"
	"sync"
)

// SubjectEvent is the event being fired
type SubjectEvent interface{}

// SubjectChannel is the channel that will pass the events
type SubjectChannel chan SubjectEvent

// Subscriber represents the base functionality of a subject (used by observers)
type Subscriber interface {
	Register(id string) (SubjectChannel, error)
	Deregister(id string)
}

// Publisher introduces "write" capabilities on the subject
type Publisher interface {
	Notify(e SubjectEvent)
}

// Subject represent the interface for a subject
type Subject interface {
	Subscriber
	Publisher
}

// subject is the internal implementation of Subject
type subject struct {
	observers map[string]*observer
	mut       sync.Mutex
}

// NewSubject creates a new instance of the internal struct
func NewSubject() Subject {
	outs := map[string]*observer{}
	s := subject{outs, sync.Mutex{}}
	return &s
}

// Register adds a new observer
func (s *subject) Register(id string) (SubjectChannel, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	if ob, exist := s.observers[id]; exist {
		return ob.channel, errors.New("observer already exist")
	}
	s.observers[id] = newSubjectObserver()
	return s.observers[id].channel, nil
}

// Deregister removes an observer
func (s *subject) Deregister(id string) {
	s.mut.Lock()
	defer s.mut.Unlock()

	if ob, exist := s.observers[id]; exist {
		delete(s.observers, id)
		go ob.close()
	}
}

// Notify emits an event
func (s *subject) Notify(e SubjectEvent) {
	s.mut.Lock()
	defer s.mut.Unlock()

	for _, ob := range s.observers {
		ob.notifyCallback(e)
	}
}
