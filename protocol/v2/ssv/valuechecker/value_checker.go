package valuechecker

import (
	"bytes"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/types"
)

type ValueChecker struct {
	signer         types.BeaconSigner
	network        types.BeaconNetwork
	validatorPK    types.ValidatorPK
	validatorIndex phase0.ValidatorIndex
	sharePublicKey []byte

	currentAttestationData *phase0.AttestationData
	currentBlockHashRoot   [32]byte
}

func New(
	signer types.BeaconSigner,
	network types.BeaconNetwork,
	validatorPK types.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePublicKey []byte,
) *ValueChecker {
	return &ValueChecker{
		signer:         signer,
		network:        network,
		validatorPK:    validatorPK,
		validatorIndex: validatorIndex,
		sharePublicKey: sharePublicKey,
	}
}

func (vc *ValueChecker) checkDuty(duty *types.Duty, expectedType types.BeaconRole) error {
	if vc.network.EstimatedEpochAtSlot(duty.Slot) > vc.network.EstimatedCurrentEpoch()+1 {
		return fmt.Errorf("duty epoch is into far future")
	}

	if expectedType != duty.Type {
		return fmt.Errorf("wrong beacon role type")
	}

	if !bytes.Equal(vc.validatorPK, duty.PubKey[:]) {
		return fmt.Errorf("wrong validator pk")
	}

	if vc.validatorIndex != duty.ValidatorIndex {
		return fmt.Errorf("wrong validator index")
	}

	return nil
}
