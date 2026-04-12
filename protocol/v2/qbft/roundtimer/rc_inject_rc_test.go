//go:build rc_test

package roundtimer

import (
    "log"
    "os"
    "strconv"
    "sync"
    "time"
)

var rcOnce sync.Once

func rcTestForceRC() bool { return os.Getenv("SSV_QBFT_FORCE_RC") == "1" }

func rcTestTimeout() time.Duration {
    msStr := os.Getenv("SSV_QBFT_RC_TIMEOUT_MS")
    if msStr == "" {
        return 200 * time.Millisecond
    }
    if v, err := strconv.Atoi(msStr); err == nil && v > 0 {
        return time.Duration(v) * time.Millisecond
    }
    return 200 * time.Millisecond
}

func rcTestLogClamp(enabled bool, d time.Duration) {
    if !enabled {
        return
    }
    rcOnce.Do(func() {
        log.Printf("rc_test enabled: clamped round timeout=%s", d)
    })
}

