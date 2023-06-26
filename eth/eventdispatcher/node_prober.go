package eventdispatcher

import (
	"context"
)

type nodeProber interface {
	IsReady(ctx context.Context) (bool, error)
}
