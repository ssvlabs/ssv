package scenarios

import (
	"context"
	"time"
)

// Bootstrapper creates scenario context with the relevant components
type Bootstrapper func(ctx context.Context) (*Context, error)

// Context is the context object that is passed in execution
type Context struct {
	ctx context.Context
}

// NewContext creates new context
func NewContext(ctx context.Context) *Context {
	return &Context{ctx}
}

// Ctx returns the underlying context
func (c *Context) Ctx() context.Context {
	return c.ctx
}

// WithCancel wraps context.WithCancel
func (c *Context) WithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(c.ctx)
}

// WithTimeout wraps context.WithTimeout
func (c *Context) WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, d)
}

// Value wraps context.Context.Value
func (c *Context) Value(key string) interface{} {
	return c.ctx.Value(key)
}

// WithValue wraps context.WithValue
func (c *Context) WithValue(key string, val interface{}) {
	context.WithValue(c.ctx, key, val)
}

type scenarioCfg interface {
	// NumOfOperators returns the desired number of operators for the test
	NumOfOperators() int
	// NumOfExporters returns the desired number of operators for the test
	NumOfExporters() int
	// Name is the name of the scenario
	Name() string
}

// Scenario represents a testplan for a specific scenario
type Scenario interface {
	scenarioCfg
	// PreExecution is invoked prior to the scenario, used for setup
	PreExecution(ctx *Context) error
	// Execute is the actual test scenario to run
	Execute(ctx *Context) error
	// PostExecution is invoked after execution, used for cleanup etc.
	PostExecution(ctx *Context) error
}
