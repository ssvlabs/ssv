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

type collectorRef struct {
	enabled bool
	ref Collector
}

var (
	prefix        = "ssv"
	collectors    = map[string]collectorRef{}
	collectorsMut = sync.Mutex{}
)

// Enable sets the collector enabled flag to true
func Enable(cid string) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	if cr, ok := collectors[cid]; ok {
		cr.enabled = true
		collectors[cid] = cr
	} else {
		collectors[cid] = collectorRef{enabled: true}
	}
}

// Register adds a collector to be called when metrics are collected
func Register(c Collector) {
	collectorsMut.Lock()
	defer collectorsMut.Unlock()

	if cr, ok := collectors[c.ID()]; !ok {
		collectors[c.ID()] = collectorRef{ref: c}
	} else {
		cr.ref = c
	}
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
		if !cr.enabled {
			continue
		}
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
		}(cr.ref)
	}

	collectorsWg.Wait()

	return results, errs
}

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