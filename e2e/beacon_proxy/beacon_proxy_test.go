package beaconproxy

import (
	"testing"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

func TestTest(t *testing.T) {
	m := make(map[Gateway]int)
	m[Gateway{Name: "a", Port: 1}] = 1
	m[Gateway{Name: "b", Port: 2}] = 2
	m[Gateway{Name: "c", Port: 3}] = 3
	l, _ := zap.NewDevelopment()
	l.Info("test", zap.Any("m", maps.Keys(m)))
}
