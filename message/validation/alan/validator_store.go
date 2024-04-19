package validation

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/alan/types"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type CommitteeID [32]byte

type Committee struct {
	ID         CommitteeID
	Operators  []spectypes.OperatorID
	Validators []*ssvtypes.SSVShare
}

type ValidatorStore interface {
	Validator(pubKey []byte) *ssvtypes.SSVShare
	Validators() []*ssvtypes.SSVShare
	ParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare
	OperatorValidators(id spectypes.OperatorID) []*ssvtypes.SSVShare

	Committee(id CommitteeID) *Committee
	Committees() []*Committee
	ParticipatingCommittees(epoch phase0.Epoch) []*Committee
	OperatorCommittees(id spectypes.OperatorID) []*Committee
}
