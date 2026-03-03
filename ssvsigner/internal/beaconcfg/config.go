package beaconcfg

import (
	"fmt"
	"math"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Config struct {
	Name                  string
	SlotDuration          time.Duration
	SlotsPerEpoch         uint64
	GenesisTime           time.Time
	GenesisValidatorsRoot phase0.Root
	Forks                 map[spec.DataVersion]phase0.Fork
}

func (b *Config) EstimatedCurrentSlot() phase0.Slot {
	return b.EstimatedSlotAtTime(time.Now())
}

func (b *Config) EstimatedCurrentEpoch() phase0.Epoch {
	return b.EstimatedEpochAtSlot(b.EstimatedCurrentSlot())
}

func (b *Config) EstimatedSlotAtTime(ts time.Time) phase0.Slot {
	if ts.Before(b.GenesisTime) {
		panic(fmt.Sprintf("time %v is before genesis time %v", ts, b.GenesisTime))
	}
	return phase0.Slot(ts.Sub(b.GenesisTime) / b.SlotDuration) // #nosec G115
}

func (b *Config) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(uint64(slot) / b.SlotsPerEpoch)
}

func (b *Config) FirstSlotAtEpoch(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(uint64(epoch) * b.SlotsPerEpoch)
}

func (b *Config) EpochDuration() time.Duration {
	if b.SlotsPerEpoch > math.MaxInt64 {
		panic("slots per epoch out of range")
	}
	return b.SlotDuration * time.Duration(b.SlotsPerEpoch) // #nosec G115
}

func (b *Config) ForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
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
