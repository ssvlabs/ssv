package async

import (
	"context"
	"time"
)

// Interval runs the provided function periodically every period
func Interval(pctx context.Context, interval time.Duration, f func()) {
	ticker := time.NewTicker(interval)
	go func() {
		ctx, cancel := context.WithCancel(pctx)
		defer cancel()
		for {
			select {
			case <-ticker.C:
				f()
			case <-ctx.Done():
				return
			}
		}
	}()
}
