package storage

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// legacyValidatorAdapter provides backward compatibility for existing consumers
// TODO: will be removed later, but I have to deal with this spaghetti somehow.
type legacyValidatorAdapter struct {
	store        *validatorStoreImpl
	operatorIDFn func() spectypes.OperatorID
}

func (l *legacyValidatorAdapter) OperatorValidators(operatorID spectypes.OperatorID) []*types.SSVShare {
	snapshots := l.store.GetOperatorValidators(operatorID)
	shares := make([]*types.SSVShare, len(snapshots))
	for i, snapshot := range snapshots {
		share := snapshot.Share
		shares[i] = &share
	}
	return shares
}

func (l *legacyValidatorAdapter) SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare {
	operatorID := l.operatorIDFn()
	opts := ParticipationOptions{
		IncludeLiquidated: false,
		IncludeExited:     true,
	}

	allParticipating := l.store.GetParticipatingValidators(epoch, opts)
	var filtered []*types.SSVShare

	for _, snapshot := range allParticipating {
		if snapshot.Share.BelongsToOperator(operatorID) {
			share := snapshot.Share
			filtered = append(filtered, &share)
		}
	}

	return filtered
}

func (l *legacyValidatorAdapter) Validator(pubKey []byte) (*types.SSVShare, bool) {
	var validatorPK spectypes.ValidatorPK
	copy(validatorPK[:], pubKey)

	snapshot, found := l.store.GetValidator(ValidatorPubKey(validatorPK))
	if !found {
		return nil, false
	}

	share := snapshot.Share
	return &share, true
}
