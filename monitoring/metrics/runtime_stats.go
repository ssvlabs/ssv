package metrics

import (
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"
	"runtime"
)

func reportRuntimeStats() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	logex.GetLogger(zap.String("who", "reportRuntimeStats")).Debug("mem stats",
		zap.Uint64("mem.TotalAlloc", mem.TotalAlloc),
		zap.Uint64("mem.HeapAlloc", mem.HeapAlloc),
		zap.Uint64("mem.HeapReleased", mem.HeapReleased),
		zap.Uint32("mem.NumGC", mem.NumGC),
	)
}
