package metrics

import (
	"fmt"
	"strings"
	"sync"
)

// Collector interface that represents a specific collector
type Collector interface {
	Collect() ([]string, error)
	ID() string
}

var (
	prefix        = "ssv-collect"
	collectors    = map[string]Collector{}
	collectorsMut = sync.Mutex{}
)

// Register adds a collector to be called when metrics are collected
func Register(c Collector) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	collectors[c.ID()] = c
}

// Deregister removes a collector
func Deregister(c Collector) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	delete(collectors, c.ID())
}

// Collect collects metrics from all the registered collectors
func Collect() ([]string, []error) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	var collectorsWg sync.WaitGroup

	var errsMut sync.Mutex
	var errs []error
	var resultsMut sync.Mutex
	var results []string

	for _, cr := range collectors {
		collectorsWg.Add(1)
		go func(c Collector) {
			defer collectorsWg.Done()
			metrics, err := c.Collect()
			if err != nil {
				errsMut.Lock()
				errs = append(errs, err)
				errsMut.Unlock()
				return
			}
			resultsMut.Lock()
			for _, m := range metrics {
				results = append(results, fmt.Sprintf("%s.%s.%s", prefix, c.ID(), m))
			}
			resultsMut.Unlock()
		}(cr)
	}

	collectorsWg.Wait()

	return results, errs
}

// ParseMetricsConfig takes a metrics config string and parse it to collector ids
// metricsCfg has the following pattern: {collector},{collector},....
// e.g. "validator,network"
func ParseMetricsConfig(metricsCfg string) []string {
	if len(metricsCfg) == 0 {
		return []string{}
	}
	metricsPkgs := strings.Split(metricsCfg, ",")
	var cids []string
	for _, s := range metricsPkgs {
		if len(s) > 0 {
			cids = append(cids, s)
		}
	}
	return cids
}
