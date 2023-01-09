package scenarios

import (
	"bytes"
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
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]*spectypes.Duty{
			1: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
			2: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
			3: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
			4: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, role),
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
		Identifier: identifier,
	}
}

func instanceValidator(consensusData *spectypes.ConsensusData, operatorID spectypes.OperatorID, identifier spectypes.MessageID) func(actual *protocolstorage.StoredInstance) error {
	return func(actual *protocolstorage.StoredInstance) error {
		consensusData, err := consensusData.Encode()
		if err != nil {
			panic(err)
		}

		proposalData, err := (&specqbft.ProposalData{
			Data:                     consensusData,
			RoundChangeJustification: nil,
			PrepareJustification:     nil,
		}).Encode()
		if err != nil {
			panic(err)
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

		expectedMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     specqbft.FirstHeight,
			Round:      specqbft.FirstRound,
			Identifier: identifier[:],
			Data:       proposalData,
		})
		if !isMessageExistInRound(expectedMsg, actual.State.ProposeContainer.Msgs[specqbft.FirstRound]) {
			return fmt.Errorf("poposal message %+v wasn't found at actual.State.ProposeContainer.Msgs[specqbft.FirstRound]", expectedMsg)
		}

		foundedMsgsCounter := 0 //at the end of test it must be at least 3
		for i := 1; i <= 4; i++ {
			operatorIDIterator := spectypes.OperatorID(i)

			expectedMsg = spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       prepareData,
			})
			if !isMessageExistInRound(expectedMsg, actual.State.PrepareContainer.Msgs[specqbft.FirstRound]) {
				return fmt.Errorf("prepare message %+v wasn't found at actual.State.PrepareContainer.Msgs[specqbft.FirstRound]", expectedMsg)
			}

			expectedMsg = spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       commitData,
			})
			if isMessageExistInRound(expectedMsg, actual.State.CommitContainer.Msgs[specqbft.FirstRound]) {
				foundedMsgsCounter++
			}
		}

		if !actual.State.Share.HasQuorum(foundedMsgsCounter) {
			return fmt.Errorf("wasn't found enough commit messages at actual.State.CommitContainer.Msgs[specqbft.FirstRound], expected at least %d, actual = %d", actual.State.Share.Quorum, foundedMsgsCounter)
		}

		actual.State.ProposeContainer = nil
		actual.State.PrepareContainer = nil
		actual.State.CommitContainer = nil

		expectedStoredInstance := &protocolstorage.StoredInstance{
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

		expectedStateRoot, err := expectedStoredInstance.State.GetRoot()
		if err != nil {
			return fmt.Errorf("error during geting root from expected state: %w", err)
		}

		actualStateRoot, err := actual.State.GetRoot()
		if err != nil {
			return fmt.Errorf("error during geting root from actual state: %w", err)
		}

		if !bytes.Equal(expectedStateRoot, actualStateRoot) {
			return fmt.Errorf("expected and actual state roots doesn't matching, expected state = %+v, actual = %+v", expectedStoredInstance.State, actual.State)
		}

		expectedDecidedMessageRoot, err := expectedStoredInstance.DecidedMessage.GetRoot()
		if err != nil {
			return fmt.Errorf("error during geting root from expected decided message: %w", err)
		}

		actualDecidedMessageRoot, err := actual.DecidedMessage.GetRoot()
		if err != nil {
			return fmt.Errorf("error during geting root from actual decided message: %w", err)
		}

		if !bytes.Equal(expectedDecidedMessageRoot, actualDecidedMessageRoot) {
			return fmt.Errorf("expected and actual decided message roots doesn't matching, expected decided message = %+v, actual = %+v", expectedStoredInstance.DecidedMessage, actual.DecidedMessage)
		}

		return nil
	}
}

func isMessageExistInRound(message *specqbft.SignedMessage, round []*specqbft.SignedMessage) bool {
	for i := range round {
		expectedRoot, err := message.GetRoot()
		if err != nil {
			return false
		}

		actualRoot, err := round[i].GetRoot()
		if err != nil {
			return false
		}

		if bytes.Equal(expectedRoot, actualRoot) {
			return true
		}
	}
	return false
}
