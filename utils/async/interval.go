package async

import (
	"context"
	"time"
)

// Interval runs the provided function periodically every period
func Interval(ctx context.Context, interval time.Duration, f func()) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()

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
