package utils

import "github.com/bloxapp/ssv/utils/tasks"

// Queue this interface is in protocol cause of the use in validator metadata
// Queue is an interface for event queue
type Queue interface {
	Start()
	Stop()
	Queue(fn tasks.Fn)
	QueueDistinct(tasks.Fn, string)
	Wait()
	Errors() []error
}
