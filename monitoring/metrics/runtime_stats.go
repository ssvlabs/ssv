package metrics

import (
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
	"runtime"
)

var (
	metricMemAlloc = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:runtime:mem:alloc",
		Help: "Allocated memory",
	})
	metricMemTotalAlloc = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:runtime:mem:total_alloc",
		Help: "Total allocated memory",
	})
	metricMemHeapAlloc = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:runtime:mem:heap_alloc",
		Help: "Heap allocated memory",
	})
	metricMemNumGC = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:runtime:mem:num_gc",
		Help: "Number of GCs",
	})
)

func init() {
	if err := prometheus.Register(metricMemAlloc); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricMemTotalAlloc); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricMemHeapAlloc); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricMemNumGC); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportRuntimeStats() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	logex.GetLogger(zap.String("who", "reportRuntimeStats")).Debug("mem stats",
		zap.Uint64("mem.Alloc", mem.Alloc),
		zap.Uint64("mem.TotalAlloc", mem.TotalAlloc),
		zap.Uint64("mem.HeapAlloc", mem.HeapAlloc),
		zap.Uint32("mem.NumGC", mem.NumGC),
	)
	metricMemAlloc.Set(float64(mem.Alloc))
	metricMemTotalAlloc.Set(float64(mem.TotalAlloc))
	metricMemHeapAlloc.Set(float64(mem.HeapAlloc))
	metricMemNumGC.Set(float64(mem.NumGC))
}
