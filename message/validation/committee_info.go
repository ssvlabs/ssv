package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type CommitteeInfo struct {
	committeeID      spectypes.CommitteeID
	committee        []spectypes.OperatorID
	signerIndices    map[spectypes.OperatorID]int
	validatorIndices []phase0.ValidatorIndex
	subnet           uint64
	subnetAlan       uint64
}

func committeeInfoFromCommittee(committee *registrystorage.Committee) CommitteeInfo {
	signerIndices := make(map[spectypes.OperatorID]int)
	for i, operator := range committee.Operators {
		signerIndices[operator] = i
	}

	return CommitteeInfo{
		committeeID:      committee.ID,
		committee:        committee.Operators,
		signerIndices:    signerIndices,
		validatorIndices: committee.Indices,
		subnet:           committee.Subnet,
		subnetAlan:       committee.SubnetAlan,
	}
}

func committeeInfoFromShare(share *ssvtypes.SSVShare, validatorIndices []phase0.ValidatorIndex) CommitteeInfo {
	signerIndices := make(map[spectypes.OperatorID]int)
	for i, operator := range share.OperatorIDs() {
		signerIndices[operator] = i
	}

	return CommitteeInfo{
		committeeID:      share.CommitteeID(),
		committee:        share.OperatorIDs(),
		signerIndices:    signerIndices,
		validatorIndices: validatorIndices,
		subnet:           share.CommitteeSubnet(),
		subnetAlan:       share.CommitteeSubnetAlan(),
	}
}

// keeping the method for readability and the comment
func (ci *CommitteeInfo) signerIndex(signer spectypes.OperatorID) int {
	return ci.signerIndices[signer] // existence must be checked by ErrSignerNotInCommittee
}
