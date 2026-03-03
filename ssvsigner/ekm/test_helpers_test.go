package ekm

import (
	"sync"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var initBLSOnce sync.Once

func initBLSTest() {
	initBLSOnce.Do(func() {
		_ = bls.Init(bls.BLS12_381)
		_ = bls.SetETHmode(bls.EthModeDraft07)
	})
}

func testLogger(t testing.TB) *zap.Logger {
	t.Helper()
	return zaptest.NewLogger(t)
}
