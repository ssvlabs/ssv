package queue

import (
	"context"
)

// TODO: add missing tests

// Fn represents a function to execute
type Fn func() error

// Queue this interface is in protocol cause of the use in validator metadata
// Queue is an interface for event queue
type Queue interface {
	Start(ctx context.Context)
	Stop()
	Queue(fn Fn)
	QueueDistinct(Fn, string)
	Wait()
	Errors() []error
}
