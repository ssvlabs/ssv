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
func RegularAttester(f int) *IntegrationTest {
	sharesSet = getShareSet(f)
	identifier := spectypes.NewMsgID(sharesSet.ValidatorPK.Serialize(), spectypes.BNRoleAttester)

	operatorIDs := []spectypes.OperatorID{}
	duties := map[spectypes.OperatorID][]scheduledDuty{}
	instanceValidators := map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error{}
	startDutyErrors := map[spectypes.OperatorID]error{}

	for i := 1; i <= getF3plus1(f); i++ {
		currentOperatorId := spectypes.OperatorID(i)

		operatorIDs = append(operatorIDs, currentOperatorId)
		duties[currentOperatorId] = []scheduledDuty{{Duty: createDuty(sharesSet.ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)}}
		instanceValidators[currentOperatorId] = []func(*protocolstorage.StoredInstance) error{regularAttesterInstanceValidator(currentOperatorId, identifier)}
		startDutyErrors[currentOperatorId] = nil
	}

	return &IntegrationTest{
		Name:               "regular attester",
		OperatorIDs:        operatorIDs,
		Identifier:         identifier,
		InitialInstances:   nil,
		Duties:             duties,
		InstanceValidators: instanceValidators,
		StartDutyErrors:    startDutyErrors,
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
		expectedProposeMsg := spectestingutils.SignQBFTMsg(sharesSet.Shares[1], 1, &specqbft.Message{
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
				Share:             testingShare(sharesSet, operatorID),
				ID:                identifier[:],
				Round:             specqbft.FirstRound,
				Height:            specqbft.FirstHeight,
				LastPreparedRound: specqbft.FirstRound,
				LastPreparedValue: encodedConsensusData,
				ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(sharesSet.Shares[1], 1, &specqbft.Message{
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
