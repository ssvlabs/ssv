package changeround

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/proto"
)

// uponPartialQuorum implements pipeline.Pipeline interface
type uponPartialQuorum struct {
}

// UponPartialQuorum is the constructor of uponPartialQuorum
func UponPartialQuorum() pipeline.Pipeline {
	return &uponPartialQuorum{}
}

// Run implements pipeline.Pipeline interface
//
// upon receiving a set Frc of f + 1 valid ⟨ROUND-CHANGE, λi, rj, −, −⟩ messages such that:
// 	∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rj > ri do
// 		let ⟨ROUND-CHANGE, hi, rmin, −, −⟩ ∈ Frc such that:
// 			∀⟨ROUND-CHANGE, λi, rj, −, −⟩ ∈ Frc : rmin ≤ rj
// 		ri ← rmin
// 		set timer i to running and expire after t(ri)
//		broadcast ⟨ROUND-CHANGE, λi, ri, pri, pvi⟩
func (p *uponPartialQuorum) Run(signedMessage *proto.SignedMessage) error {
	return nil // TODO
}

// Name implements pipeline.Pipeline interface
func (p *uponPartialQuorum) Name() string {
	return "upon partial quorum"
}
