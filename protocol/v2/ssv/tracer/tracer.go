package tracer

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

type Validator struct {
	Role spectypes.BeaconRole
}

func (v *Validator) Consensus(d *spectypes.ValidatorDuty, root ssz.HashRoot) {
	_ = v.Role
	_ = d.Slot
	_ = d.ValidatorIndex
	_ = d.PubKey
	_ = d.ValidatorCommitteeIndex
	_ = d.CommitteeIndex
}

func (v *Validator) Pre(d *spectypes.ValidatorDuty, root phase0.Hash32, sigs *ssv.PartialSigContainer) {
	_ = v.Role
	_ = d.ValidatorIndex
	_ = d.Slot
	_ = d.PubKey
	_ = d.ValidatorCommitteeIndex
	_ = d.CommitteeIndex
	_ = root

	for root, sigs := range sigs.Signatures[d.ValidatorIndex] {
		_ = root
		for root2, sigs2 := range sigs {
			_ = root2
			_ = sigs2
		}
	}
}

func (v *Validator) Post(d *spectypes.ValidatorDuty, sigs *ssv.PartialSigContainer) {
	_ = v.Role
	_ = d.ValidatorIndex
	_ = d.Slot
	_ = d.PubKey
	_ = d.ValidatorCommitteeIndex
	_ = d.CommitteeIndex
}
