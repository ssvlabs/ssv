package emitter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// EventData represents the data to pass, it should have a copy function
type EventData interface {
	Copy() interface{}
}

// EventHandler handles event
type EventHandler func(data EventData)

// DeregisterFunc is a function to deregister from event
type DeregisterFunc func()

// EventSubscriber is able to subscribe on events
type EventSubscriber interface {
	On(event string, handler EventHandler) DeregisterFunc
	Once(event string, handler EventHandler)
	Channel(event string) (<-chan EventData, DeregisterFunc)
}

// EventPublisher is able to notify events
type EventPublisher interface {
	Notify(event string, data EventData)
	Clear(event string)
}

// Emitter is managing events
type Emitter interface {
	EventSubscriber
	EventPublisher
}

// emitter implements Emitter
type emitter struct {
	handlers map[string]eventHandlers
	mut      sync.RWMutex
}

// NewEmitter creates a new instance of emitter
func NewEmitter() Emitter {
	return &emitter{
		handlers: map[string]eventHandlers{},
		mut:      sync.RWMutex{},
	}
}

type eventHandlers map[string]EventHandler

// Channel register to event as a channel
func (e *emitter) Channel(event string) (<-chan EventData, DeregisterFunc) {
	cn := make(chan EventData)
	ctx, cancel := context.WithCancel(context.Background())
	deregister := e.On(event, func(data EventData) {
		select {
		case <-ctx.Done():
			return
		default:
			cn <- data
		}
	})
	return cn, func() {
		cancel()
		deregister()
		go close(cn)
	}
}

// Once register once to event as a channel
func (e *emitter) Once(event string, handler EventHandler) {
	var once sync.Once
	var deregister DeregisterFunc
	deregister = e.On(event, func(data EventData) {
		once.Do(func() {
			defer deregister()
			handler(data)
		})
	})
}

// On register to event, returns a function for de-registration
func (e *emitter) On(event string, handler EventHandler) DeregisterFunc {
	e.mut.Lock()
	defer e.mut.Unlock()

	var handlers eventHandlers
	var ok bool
	if handlers, ok = e.handlers[event]; !ok {
		handlers = make(eventHandlers)
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", time.Now().String(), len(handlers))))
	hid := hex.EncodeToString(h[:])
	handlers[hid] = handler
	e.handlers[event] = handlers

	return func() {
		e.mut.Lock()
		defer e.mut.Unlock()

		if _handlers, ok := e.handlers[event]; ok {
			delete(_handlers, hid)
			e.handlers[event] = _handlers
		}
	}
}

// Notify notifies on event
func (e *emitter) Notify(event string, data EventData) {
	e.mut.RLock()
	defer e.mut.RUnlock()

	handlers, ok := e.handlers[event]
	if !ok {
		return
	}
	for _, handler := range handlers {
		go handler(data.Copy().(EventData))
	}
}

// Clear handlers for event
func (e *emitter) Clear(event string) {
	e.mut.Lock()
	defer e.mut.Unlock()

	delete(e.handlers, event)
}
