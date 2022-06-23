package spectest

import (
	tests2 "github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests/consensus/aggregator"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests/consensus/attester"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests/consensus/proposer"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests/consensus/synccommittee"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests/consensus/synccommitteecontribution"
)

var AllTests = []*tests2.SpecTest{
	//postconsensus.ValidMessage(),
	//postconsensus.InvaliSignature(),
	//postconsensus.WrongSigningRoot(),
	//postconsensus.WrongBeaconChainSig(),
	//postconsensus.FutureConsensusState(),
	//postconsensus.PastConsensusState(),
	//postconsensus.MsgAfterReconstruction(),
	//postconsensus.DuplicateMsg(),
	//
	//messages.NoMessageSigners(),
	//messages.MultipleSigners(),
	//messages.MultipleMessageSigners(),
	//messages.NoSigners(),
	//messages.WrongMsgID(),
	//messages.UnknownMsgType(),
	//messages.NoData(),
	//messages.NoDutyRunner(),
	//
	//valcheck.WrongDutyPubKey(),

	attester.HappyFlow(),
	attester.SevenOperators(),
	//attestations.FarFutureDuty(),
	//attestations.DutySlotNotMatchingAttestationSlot(),
	//attestations.DutyCommitteeIndexNotMatchingAttestations(),
	//attestations.FarFutureAttestationTarget(),
	//attestations.AttestationSourceValid(),
	//attestations.DutyTypeWrong(),
	//attestations.AttestationDataNil(),
	//
	//processmsg.NoData(),
	//processmsg.InvalidConsensusMsg(),
	//processmsg.InvalidDecidedMsg(),
	//processmsg.InvalidPostConsensusMsg(),
	//processmsg.UnknownType(),
	//processmsg.WrongPubKey(),
	//processmsg.WrongBeaconType(),

	proposer.HappyFlow(),
	proposer.SevenOperators(),

	aggregator.HappyFlow(),
	aggregator.SevenOperators(),

	synccommittee.HappyFlow(),
	synccommittee.SevenOperators(),

	synccommitteecontribution.HappyFlow(),
	synccommitteecontribution.SevenOperators(),
}
