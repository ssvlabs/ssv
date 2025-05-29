package slotticker

import "time"

type TimerProvider func(d time.Duration) Timer

type Timer interface {
	Stop() bool
	Reset(d time.Duration) bool
	C() <-chan time.Time
}

type timer struct {
	*time.Timer
}

func NewTimer(d time.Duration) Timer {
	return &timer{time.NewTimer(d)}
}

func (t *timer) C() <-chan time.Time {
	return t.Timer.C
}
