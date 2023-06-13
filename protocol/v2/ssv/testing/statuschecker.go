package testing

import (
	"context"
)

type StatusChecker struct {
}

func (StatusChecker) IsReady(context.Context) (bool, error) {
	return true, nil
}
