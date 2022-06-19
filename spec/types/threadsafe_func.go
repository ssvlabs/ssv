package types

import "sync"

// ThreadSafeF makes function execution thread safe
type ThreadSafeF struct {
	t sync.Mutex
}

// NewThreadSafeF returns a new instance of NewThreadSafeF
func NewThreadSafeF() *ThreadSafeF {
	return &ThreadSafeF{
		t: sync.Mutex{},
	}
}

// Run runs the provided function
func (safeF *ThreadSafeF) Run(f func() interface{}) interface{} {
	safeF.t.Lock()
	defer safeF.t.Unlock()
	return f()
}
