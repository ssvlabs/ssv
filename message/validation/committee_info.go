package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type CommitteeInfo struct {
	committeeID      spectypes.CommitteeID
	committee        []spectypes.OperatorID
	signerIndices    map[spectypes.OperatorID]int
	validatorIndices []phase0.ValidatorIndex
	subnet           commons.Subnet
	subnetAlan       commons.Subnet
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
		subnet:           committee.BooleSubnet,
		subnetAlan:       committee.AlanSubnet,
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
		subnet:           share.BooleCommitteeSubnet(),
		subnetAlan:       share.AlanCommitteeSubnet(),
	}
}

// keeping the method for readability and the comment
func (ci *CommitteeInfo) signerIndex(signer spectypes.OperatorID) int {
	return ci.signerIndices[signer] // existence must be checked by ErrSignerNotInCommittee
}
