package format

import (
	"regexp"
	"sync"
)

// RegexpPool holds a sync.Pool of *regexp.Regexp for a specific pattern
type RegexpPool struct {
	pool    sync.Pool
	pattern string
}

// NewRegexpPool creates a new instance of RegexpPool
func NewRegexpPool(pattern string) *RegexpPool {
	return &RegexpPool{
		pool: sync.Pool{New: func() any {
			return regexp.MustCompile(pattern)
		}},
		pattern: pattern,
	}
}

// Get returns an instance of the regexp and a callback function that adds it back to the pool
func (r *RegexpPool) Get() (*regexp.Regexp, func()) {
	re, ok := r.pool.Get().(*regexp.Regexp)
	if !ok {
		re = regexp.MustCompile(r.pattern)
	}
	return re, func() {
		r.pool.Put(re)
	}
}
