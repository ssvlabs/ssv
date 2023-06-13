package testing

import (
	"context"
)

type SuccessfulStatusChecker struct {
}

func (SuccessfulStatusChecker) IsReady(context.Context) (bool, error) {
	return true, nil
}
