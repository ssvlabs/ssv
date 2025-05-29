package beaconproxy

import (
	"maps"
	"slices"
	"testing"

	"go.uber.org/zap"
)

func TestTest(t *testing.T) {
	m := make(map[Gateway]int)
	m[Gateway{Name: "a", Port: 1}] = 1
	m[Gateway{Name: "b", Port: 2}] = 2
	m[Gateway{Name: "c", Port: 3}] = 3
	l, _ := zap.NewDevelopment()
	l.Info("test", zap.Any("m", slices.Collect(maps.Keys(m))))
}
