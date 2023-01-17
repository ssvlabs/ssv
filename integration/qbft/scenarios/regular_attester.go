package scenarios

import (
	"fmt"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// RegularAttester integration test.
// TODO: consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
func RegularAttester() *IntegrationTest {
	identifier := spectypes.NewMsgID(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectypes.BNRoleAttester)

	return &IntegrationTest{
		Name:             "regular attester",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		Identifier:       identifier,
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]scheduledDuty{
			1: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)}},
			2: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)}},
			3: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)}},
			4: {scheduledDuty{Duty: createDuty(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)}},
		},
		InstanceValidators: map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error{
			1: {
				regularAttesterInstanceValidator(1, identifier),
			},
			2: {
				regularAttesterInstanceValidator(2, identifier),
			},
			3: {
				regularAttesterInstanceValidator(3, identifier),
			},
			4: {
				regularAttesterInstanceValidator(4, identifier),
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

func regularAttesterInstanceValidator(operatorID spectypes.OperatorID, identifier spectypes.MessageID) func(actual *protocolstorage.StoredInstance) error {
	return func(actual *protocolstorage.StoredInstance) error {
		encodedConsensusData, err := spectestingutils.TestAttesterConsensusData.Encode()
		if err != nil {
			return fmt.Errorf("encode consensus data: %w", err)
		}

		proposalData, err := (&specqbft.ProposalData{
			Data:                     encodedConsensusData,
			RoundChangeJustification: nil,
			PrepareJustification:     nil,
		}).Encode()
		if err != nil {
			return fmt.Errorf("encode proposal data: %w", err)
		}

		prepareData, err := (&specqbft.PrepareData{
			Data: encodedConsensusData,
		}).Encode()
		if err != nil {
			return fmt.Errorf("encode prepare data: %w", err)
		}

		commitData, err := (&specqbft.CommitData{
			Data: encodedConsensusData,
		}).Encode()
		if err != nil {
			return fmt.Errorf("encode commit data: %w", err)
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
			return fmt.Errorf("propose message mismatch: %w", err)
		}

		// sometimes there may be no prepare quorum TODO add quorum check after fixes
		_, prepareMessages := actual.State.PrepareContainer.LongestUniqueSignersForRoundAndValue(specqbft.FirstRound, prepareData)

		expectedPrepareMsg := &specqbft.SignedMessage{
			Message: &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       prepareData,
			},
		}
		for i, actualPrepareMessage := range prepareMessages {
			if err := validateSignedMessage(expectedPrepareMsg, actualPrepareMessage); err != nil {
				return fmt.Errorf("prepare message root mismatch, index %d", i)
			}
		}

		commitSigners, commitMessages := actual.State.CommitContainer.LongestUniqueSignersForRoundAndValue(specqbft.FirstRound, commitData)
		if !actual.State.Share.HasQuorum(len(commitSigners)) {
			return fmt.Errorf("no commit message quorum, signers: %v", commitSigners)
		}

		expectedCommitMsg := &specqbft.SignedMessage{
			Message: &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       commitData,
			},
		}
		for i, actualCommitMessage := range commitMessages {
			if err := validateSignedMessage(expectedCommitMsg, actualCommitMessage); err != nil {
				return fmt.Errorf("commit message root mismatch, index %d", i)
			}
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
				LastPreparedValue: encodedConsensusData,
				ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
					MsgType:    specqbft.ProposalMsgType,
					Height:     specqbft.FirstHeight,
					Round:      specqbft.FirstRound,
					Identifier: identifier[:],
					Data:       proposalData,
				}),
				Decided:      true,
				DecidedValue: encodedConsensusData,

				RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
			},
			DecidedMessage: &specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     specqbft.FirstHeight,
					Round:      specqbft.FirstRound,
					Identifier: identifier[:],
					Data:       spectestingutils.PrepareDataBytes(encodedConsensusData),
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
