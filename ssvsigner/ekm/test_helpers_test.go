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

type testBeacon struct {
	Name                  string
	SlotDuration          time.Duration
	SlotsPerEpoch         uint64
	GenesisTime           time.Time
	GenesisValidatorsRoot phase0.Root
	Forks                 map[spec.DataVersion]phase0.Fork
}

func testBeaconConfig() *testBeacon {
	return &testBeacon{
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

func (b *testBeacon) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

func (b *testBeacon) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

func (b *testBeacon) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(uint64(slot) / b.SlotsPerEpoch)
}

func (b *testBeacon) EstimatedSlotAtTime(ts time.Time) phase0.Slot {
	return phase0.Slot(ts.Sub(b.GenesisTime) / b.SlotDuration) // #nosec G115
}

func (b *testBeacon) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.SlotsPerEpoch)
}

func (b *testBeacon) EpochDuration() time.Duration {
	return b.SlotDuration * time.Duration(b.SlotsPerEpoch)
}

func (b *testBeacon) ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
	}

	for i, v := range versions {
		if epoch < b.Forks[v].Epoch {
			if i == 0 {
				panic("epoch before genesis")
			}
			version := versions[i-1]
			fork := b.Forks[version]
			return version, &fork
		}
	}

	version := versions[len(versions)-1]
	fork := b.Forks[version]
	return version, &fork
}
