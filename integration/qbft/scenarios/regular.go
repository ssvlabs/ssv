package scenarios

import (
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// Regular integration test.
// TODO: consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
func Regular(role spectypes.BeaconRole) *IntegrationTest {
	identifier := spectypes.NewMsgID(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), role)

	consensusData := &spectypes.ConsensusData{
		Duty:                      createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role)[0],
		AttestationData:           spectestingutils.TestingAttestationData,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
	}

	return &IntegrationTest{
		Name:             "regular",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		Identifier:       identifier,
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]scheduledDuty{
			1: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role)}},
			2: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role)}},
			3: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role)}},
			4: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role)}},
		},
		InstanceValidators: map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error{
			1: {
				instanceValidator(consensusData, 1, identifier),
			},
			2: {
				instanceValidator(consensusData, 2, identifier),
			},
			3: {
				instanceValidator(consensusData, 3, identifier),
			},
			4: {
				instanceValidator(consensusData, 4, identifier),
			},
		},
		StartDutyErrors: map[spectypes.OperatorID]error{
			1: nil,
			2: nil,
			3: nil,
			4: nil,
		},
	}
}

func instanceValidator(consensusData *spectypes.ConsensusData, operatorID spectypes.OperatorID, identifier spectypes.MessageID) func(actual *protocolstorage.StoredInstance) error {
	return func(actual *protocolstorage.StoredInstance) error {
		consensusData, err := consensusData.Encode()
		if err != nil {
			return fmt.Errorf("error during encoding specqbft.ConsensusData: %w", err)
		}

		proposalData, err := (&specqbft.ProposalData{
			Data:                     consensusData,
			RoundChangeJustification: nil,
			PrepareJustification:     nil,
		}).Encode()
		if err != nil {
			return fmt.Errorf("error during encoding specqbft.ProposalData: %w", err)
		}

		if len(actual.State.ProposeContainer.Msgs[specqbft.FirstRound]) != 1 {
			return fmt.Errorf("propose countainer expected lenth = 1, actual = %d", len(actual.State.ProposeContainer.Msgs[specqbft.FirstRound]))
		}
		expectedProposeMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
			MsgType: specqbft.ProposalMsgType,
			Height:  specqbft.FirstHeight,
			Round:   specqbft.FirstRound,
		})
		if err := validateSignedMessage(expectedProposeMsg, actual.State.ProposeContainer.Msgs[specqbft.FirstRound][0]); err != nil { // 0 - means expected always shall be on 0 index
			return err
		}

		foundedPreparedMsgsCounter := 0 //at the end of test it must be at least == Quorum
		foundedCommitMsgsCounter := 0   //at the end of test it must be at least == Quorum
		for i := 1; i <= 4; i++ {
			operatorIDIterator := spectypes.OperatorID(i)

			expectedPreparedMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType: specqbft.PrepareMsgType,
				Height:  specqbft.FirstHeight,
				Round:   specqbft.FirstRound,
			})
			if isMessageExistInRound(expectedPreparedMsg, actual.State.PrepareContainer.Msgs[specqbft.FirstRound]) {
				foundedPreparedMsgsCounter++
			}

			expectedCommitMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType: specqbft.CommitMsgType,
				Height:  specqbft.FirstHeight,
				Round:   specqbft.FirstRound,
			})
			if isMessageExistInRound(expectedCommitMsg, actual.State.CommitContainer.Msgs[specqbft.FirstRound]) {
				foundedCommitMsgsCounter++
			}
		}

		if !actual.State.Share.HasQuorum(foundedPreparedMsgsCounter) {
			return fmt.Errorf("not enough messages in prepare container. expected = %d, actual = %d", actual.State.Share.Quorum, foundedPreparedMsgsCounter)
		}

		if !actual.State.Share.HasQuorum(foundedCommitMsgsCounter) {
			return fmt.Errorf("not enough messages in commit container. expected = %d, actual = %d", actual.State.Share.Quorum, foundedCommitMsgsCounter)
		}

		actual.State.ProposeContainer = nil
		actual.State.PrepareContainer = nil
		actual.State.CommitContainer = nil

		expected := &protocolstorage.StoredInstance{
			State: &specqbft.State{
				Share:             testingShare(spectestingutils.Testing4SharesSet(), operatorID),
				ID:                identifier[:],
				Round:             specqbft.FirstRound,
				Height:            specqbft.FirstHeight,
				LastPreparedRound: specqbft.FirstRound,
				LastPreparedValue: consensusData,
				ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
					MsgType:    specqbft.ProposalMsgType,
					Height:     specqbft.FirstHeight,
					Round:      specqbft.FirstRound,
					Identifier: identifier[:],
					Data:       proposalData,
				}),
				Decided:      true,
				DecidedValue: consensusData,

				RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
			},
			DecidedMessage: &specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     specqbft.FirstHeight,
					Round:      specqbft.FirstRound,
					Identifier: identifier[:],
					Data:       spectestingutils.PrepareDataBytes(consensusData),
				},
			},
		}

		if err := validateByRoot(expected.State, actual.State); err != nil {
			return err
		}

		if err := validateByRoot(expected.DecidedMessage, actual.DecidedMessage); err != nil {
			return err
		}

		return nil
	}
}

// validateSignedMessage compared specqbft.SignedMessage-s, !!! IGNORES .Message.Identifier and .Message.Data !!! checks only first signer !!!
func validateSignedMessage(expected, actual *specqbft.SignedMessage) error {
	expected.Message.Identifier = nil //if someone add this fields by mistake
	expected.Message.Data = nil       //if someone add this fields by mistake

	actual.Message.Identifier = nil
	actual.Message.Data = nil

	for i := range expected.Signers {
		if expected.Signers[i] != actual.Signers[i] {
			return fmt.Errorf("signers not matching. expected = %+v, actual = %+v", expected.Signers, actual.Signers)
		}
	}

	if err := validateByRoot(expected, actual); err != nil &&
		expected.Signers[0] == actual.Signers[0] {
		return err
	}

	return nil
}

func isMessageExistInRound(message *specqbft.SignedMessage, round []*specqbft.SignedMessage) bool {
	for i := range round {
		if err := validateSignedMessage(message, round[i]); err == nil {
			return true
		}
	}
	return false
}
