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
				regularInstanceValidator(consensusData, 1, identifier),
			},
			2: {
				regularInstanceValidator(consensusData, 2, identifier),
			},
			3: {
				regularInstanceValidator(consensusData, 3, identifier),
			},
			4: {
				regularInstanceValidator(consensusData, 4, identifier),
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

func regularInstanceValidator(consensusData *spectypes.ConsensusData, operatorID spectypes.OperatorID, identifier spectypes.MessageID) func(actual *protocolstorage.StoredInstance) error {
	return func(actual *protocolstorage.StoredInstance) error {
		consensusData, err := consensusData.Encode()
		if err != nil {
			return fmt.Errorf("encode consensus data: %w", err)
		}

		proposalData, err := (&specqbft.ProposalData{
			Data:                     consensusData,
			RoundChangeJustification: nil,
			PrepareJustification:     nil,
		}).Encode()
		if err != nil {
			return fmt.Errorf("encode proposal data: %w", err)
		}

		prepareData, err := (&specqbft.PrepareData{
			Data: consensusData,
		}).Encode()
		if err != nil {
			panic(err)
		}

		commitData, err := (&specqbft.CommitData{
			Data: consensusData,
		}).Encode()
		if err != nil {
			panic(err)
		}

		if len(actual.State.ProposeContainer.Msgs[specqbft.FirstRound]) != 1 {
			return fmt.Errorf("propose container expected length = 1, actual = %d", len(actual.State.ProposeContainer.Msgs[specqbft.FirstRound]))
		}
		expectedProposeMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     specqbft.FirstHeight,
			Round:      specqbft.FirstRound,
			Identifier: identifier[:],
			Data:       proposalData,
		})
		if err := validateSignedMessage(expectedProposeMsg, actual.State.ProposeContainer.Msgs[specqbft.FirstRound][0]); err != nil { // 0 - means expected always shall be on 0 index
			return err
		}

		foundPreparedMsgsCounter := 0 //at the end of test it must be at least == Quorum
		foundCommitMsgsCounter := 0   //at the end of test it must be at least == Quorum
		for i := 1; i <= 4; i++ {
			operatorIDIterator := spectypes.OperatorID(i)

			expectedPreparedMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       prepareData,
			})
			if isMessageExistInRound(expectedPreparedMsg, actual.State.PrepareContainer.Msgs[specqbft.FirstRound]) {
				foundPreparedMsgsCounter++
			}

			expectedCommitMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       commitData,
			})
			if isMessageExistInRound(expectedCommitMsg, actual.State.CommitContainer.Msgs[specqbft.FirstRound]) {
				foundCommitMsgsCounter++
			}
		}

		if !actual.State.Share.HasQuorum(foundPreparedMsgsCounter) {
			return fmt.Errorf("not enough messages in prepare container. expected = %d, actual = %d", actual.State.Share.Quorum, foundPreparedMsgsCounter)
		}

		if !actual.State.Share.HasQuorum(foundCommitMsgsCounter) {
			return fmt.Errorf("not enough messages in commit container. expected = %d, actual = %d", actual.State.Share.Quorum, foundCommitMsgsCounter)
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

func validateSignedMessage(expected, actual *specqbft.SignedMessage) error {
	for i := range expected.Signers {
		//TODO: add also specqbft.SignedMessage.Signature check
		if expected.Signers[i] != actual.Signers[i] {
			return fmt.Errorf("signers not matching. expected = %+v, actual = %+v", expected.Signers, actual.Signers)
		}
	}

	if err := validateByRoot(expected, actual); err != nil {
		return err
	}

	return nil
}

func isMessageExistInRound(message *specqbft.SignedMessage, roundMsgs []*specqbft.SignedMessage) bool {
	for i := range roundMsgs {
		if err := validateSignedMessage(message, roundMsgs[i]); err == nil {
			return true
		}
	}
	return false
}
