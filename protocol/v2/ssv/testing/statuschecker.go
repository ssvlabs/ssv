package testing

import (
	"context"
)

type StatusChecker struct {
}

func (t StatusChecker) IsReady(ctx context.Context) (bool, error) {
	return true, nil
}
