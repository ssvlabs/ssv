package ekm

import (
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/ssvsigner/internal/beaconcfg"
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

func testBeaconConfig() *beaconcfg.Config {
	return &beaconcfg.Config{
		Name:                  "testnet",
		SlotDuration:          12 * time.Second,
		SlotsPerEpoch:         32,
		GenesisTime:           time.Unix(1577836800, 0), // 2020-01-01 00:00:00 UTC
		GenesisValidatorsRoot: phase0.Root{0x04, 0x3d, 0xb0, 0xd9, 0xa8, 0x38, 0x13, 0x55, 0x1e, 0xe2, 0xf3, 0x34, 0x50, 0xd2, 0x37, 0x97, 0x75, 0x7d, 0x43, 0x09, 0x11, 0xa9, 0x32, 0x05, 0x30, 0xad, 0x8a, 0x0e, 0xab, 0xc4, 0x3e, 0xfb},
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch:           0,
				PreviousVersion: phase0.Version{0, 0, 0, 0},
				CurrentVersion:  phase0.Version{0, 0, 0, 0},
			},
			spec.DataVersionAltair: {
				Epoch:           1,
				PreviousVersion: phase0.Version{0, 0, 0, 0},
				CurrentVersion:  phase0.Version{1, 0, 0, 0},
			},
			spec.DataVersionBellatrix: {
				Epoch:           2,
				PreviousVersion: phase0.Version{1, 0, 0, 0},
				CurrentVersion:  phase0.Version{2, 0, 0, 0},
			},
			spec.DataVersionCapella: {
				Epoch:           3,
				PreviousVersion: phase0.Version{2, 0, 0, 0},
				CurrentVersion:  phase0.Version{3, 0, 0, 0},
			},
			spec.DataVersionDeneb: {
				Epoch:           4,
				PreviousVersion: phase0.Version{3, 0, 0, 0},
				CurrentVersion:  phase0.Version{4, 0, 0, 0},
			},
			spec.DataVersionElectra: {
				Epoch:           5,
				PreviousVersion: phase0.Version{4, 0, 0, 0},
				CurrentVersion:  phase0.Version{5, 0, 0, 0},
			},
			spec.DataVersionFulu: {
				Epoch:           6,
				PreviousVersion: phase0.Version{5, 0, 0, 0},
				CurrentVersion:  phase0.Version{6, 0, 0, 0},
			},
		},
	}
}
