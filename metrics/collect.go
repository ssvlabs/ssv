package metrics

import (
	"fmt"
	"sync"
)

// Collector interface that represents a specific collector
type Collector interface {
	Collect() ([]string, error)
	ID() string
}

var (
	prefix        = "ssv"
	collectors    = map[string]Collector{}
	collectorsMut = sync.RWMutex{}
)

func Register(c Collector) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	collectors[c.ID()] = c
}

func Collect() ([]string, error) {
	collectorsMut.RLock()
	defer collectorsMut.RUnlock()

	var results []string
	for _, c := range collectors {
		metrics, err := c.Collect()
		if err != nil {
			return nil, err
		}
		for i, _ := range metrics {
			metrics[i] = fmt.Sprintf("%s.%s.%s", prefix, c.ID(), metrics[i])
		}
		results = append(results, metrics...)
	}
	return results, nil
}
