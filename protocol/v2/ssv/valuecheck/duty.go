package valuecheck

import (
	"bytes"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func dutyValueCheck(
	duty *spectypes.BeaconDuty,
	network spectypes.BeaconNetwork,
	expectedType spectypes.BeaconRole,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) error {
	if network.EstimatedEpochAtSlot(duty.Slot) > network.EstimatedCurrentEpoch()+1 {
		return errors.New("duty epoch is into far future")
	}

	if expectedType != duty.Type {
		return errors.New("wrong beacon role type")
	}

	if !bytes.Equal(validatorPK[:], duty.PubKey[:]) {
		return errors.New("wrong validator pk")
	}

	if validatorIndex != duty.ValidatorIndex {
		return errors.New("wrong validator index")
	}

	return nil
}
