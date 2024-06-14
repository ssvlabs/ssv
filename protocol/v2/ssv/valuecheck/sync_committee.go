package valuecheck

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func SyncCommitteeValueCheckF(
	signer spectypes.BeaconSigner,
	network spectypes.BeaconNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleSyncCommittee, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}
		return nil
	}
}
