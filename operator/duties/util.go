package duties

import (
	"context"
)

// ctxWithParentDeadline returns newly created context setting its deadline to parentCtx
// deadline if it has one set. This is useful when we want to inherit parentCtx deadline,
// but not get canceled when parentCtx is canceled (e.g., we want to keep working in the
// background until the deadline expires).
func ctxWithParentDeadline(parentCtx context.Context) (ctx context.Context, cancel context.CancelFunc, withDeadline bool) {
	ctx, cancel = context.WithoutCancel(parentCtx), func() {}
	parentDeadline, ok := parentCtx.Deadline()
	if ok {
		ctx, cancel = context.WithDeadline(context.WithoutCancel(parentCtx), parentDeadline)
		withDeadline = true
	}
	return ctx, cancel, withDeadline
}
