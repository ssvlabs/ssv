package slashinginterceptor

import (
	"fmt"

	"crypto/rand"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var ProposerSlashingTests = []ProposerSlashingTest{
	{
		Name:      "HigherSlot_DifferentRoot",
		Slashable: false,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot++
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "SameSlot_DifferentRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				_, err := rand.Read(block.Capella.ParentRoot[:])
				return err
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
		},
	},
	{
		Name:      "LowerSlot_SameRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot--
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			return nil
		},
	},
}

var AttesterSlashingTests = []AttesterSlashingTest{
	{
		Name:      "SameSource_HigherTarget_DifferentRoot",
		Slashable: false,
		Apply: func(data *phase0.AttestationData) error {
			data.Target.Epoch += startEndEpochsDiff
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "SameSource_SameTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			return nil
		},
	},
	{
		Name:      "SameSource_SameTarget_DifferentRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
	//{
	//	Name:      "HigherSource_SameTarget_SameRoot",
	//	Slashable: true,
	//	Apply: func(data *phase0.AttestationData) error {
	//		data.Source.Epoch += startEndEpochsDiff
	//		return nil
	//	},
	//},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
}
