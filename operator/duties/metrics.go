package duties

import (
	"time"
)

type Metrics interface {
	SlotDelay(delay time.Duration)
}

type nopMetrics struct{}

func (n nopMetrics) SlotDelay(delay time.Duration) {}
