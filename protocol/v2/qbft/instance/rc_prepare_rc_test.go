//go:build rc_test

package instance

import (
    "os"
    "strconv"
    "sync"

    "go.uber.org/zap"
)

var rcPrepOnce sync.Once

func rcShouldDropPrepare() bool { return os.Getenv("SSV_QBFT_DROP_PREPARE") == "1" }

func rcMaxRounds() int {
    v := os.Getenv("SSV_QBFT_RC_ROUNDS")
    if v == "" { return 0 }
    if n, err := strconv.Atoi(v); err == nil && n > 0 { return n }
    return 0
}

func rcLogConfigIf(logger *zap.Logger) {
    rcPrepOnce.Do(func() {
        if logger != nil {
            logger.Warn("rc_test enabled",
                zap.Bool("dropPrepare", rcShouldDropPrepare()),
                zap.Int("rcRoundsCap", rcMaxRounds()),
            )
        }
    })
}

