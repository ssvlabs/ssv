package scenarios

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

func RoundChange(role spectypes.BeaconRole) *IntegrationTest {
	pk := spectestingutils.Testing4SharesSet().ValidatorPK.Serialize()
	identifier := spectypes.NewMsgID(pk, role)

	slots := []spec.Slot{
		spec.Slot(spectestingutils.TestingDutySlot + 0),
		spec.Slot(spectestingutils.TestingDutySlot + 1),
		spec.Slot(spectestingutils.TestingDutySlot + 2),
		spec.Slot(spectestingutils.TestingDutySlot + 3),
	}

	delays := []time.Duration{
		5 * time.Millisecond,
		8000 * time.Millisecond,
		16000 * time.Millisecond,
		24000 * time.Millisecond,
	}

	data := &spectypes.ConsensusData{
		Duty:                      createDuty(pk, slots[3], 1, role),
		AttestationData:           spectestingutils.TestingAttestationData,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
	}

	consensusData, err := data.Encode()
	if err != nil {
		panic(err)
	}

	return &IntegrationTest{
		Name:             "round change",
		OperatorIDs:      []spectypes.OperatorID{1, 2, 3, 4},
		InitialInstances: nil,
		Duties: map[spectypes.OperatorID][]scheduledDuty{
			1: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			2: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[1], 1, role, delays[1]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			3: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
			4: {createScheduledDuty(pk, slots[0], 1, role, delays[0]), createScheduledDuty(pk, slots[2], 1, role, delays[2]), createScheduledDuty(pk, slots[3], 1, role, delays[3])},
		},
		// TODO: just check state for 3rd duty
		InstanceValidators: map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error{
			1: {
				roundChangeInstanceValidator(consensusData, 1, identifier),
			},
			2: {
				roundChangeInstanceValidator(consensusData, 2, identifier),
			},
			3: {
				roundChangeInstanceValidator(consensusData, 3, identifier),
			},
			4: {
				roundChangeInstanceValidator(consensusData, 4, identifier),
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

func roundChangeInstanceValidator(consensusData []byte, operatorID spectypes.OperatorID, identifier spectypes.MessageID) func(actual *protocolstorage.StoredInstance) error {
	return func(actual *protocolstorage.StoredInstance) error {

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
		expectedProposeMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     2,
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
				Height:     2,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       prepareData,
			})
			if isMessageExistInRound(expectedPreparedMsg, actual.State.PrepareContainer.Msgs[specqbft.FirstRound]) {
				foundPreparedMsgsCounter++
			}

			expectedCommitMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorIDIterator], operatorIDIterator, &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     2,
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
				Height:            2,
				LastPreparedRound: specqbft.FirstRound,
				LastPreparedValue: consensusData,
				ProposalAcceptedForCurrentRound: spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
					MsgType:    specqbft.ProposalMsgType,
					Height:     2,
					Round:      specqbft.FirstRound,
					Identifier: identifier[:],
					Data:       proposalData,
				}),
				Decided:              true,
				DecidedValue:         consensusData,
				RoundChangeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{}},
			},
			DecidedMessage: &specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     2,
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

func messageDataForSlot(role spectypes.BeaconRole, pk []byte, slot spec.Slot) (consensusData, proposalData, prepareData, commitData, roundChangeData []byte, err error) {
	data := &spectypes.ConsensusData{
		Duty:                      createDuty(pk, slot, 1, role),
		AttestationData:           spectestingutils.TestingAttestationData,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    spec.Root{},
		SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{},
	}

	data.AttestationData.Slot = slot

	consensusData, err = data.Encode()
	if err != nil {
		return
	}

	proposalData, err = (&specqbft.ProposalData{
		Data:                     consensusData,
		RoundChangeJustification: nil,
		PrepareJustification:     nil,
	}).Encode()
	if err != nil {
		return
	}

	prepareData, err = (&specqbft.PrepareData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		return
	}

	commitData, err = (&specqbft.CommitData{
		Data: consensusData,
	}).Encode()
	if err != nil {
		return
	}

	roundChangeData, err = (&specqbft.RoundChangeData{
		PreparedRound:            0,
		PreparedValue:            nil,
		RoundChangeJustification: nil,
	}).Encode()
	if err != nil {
		return
	}

	return consensusData, proposalData, prepareData, commitData, roundChangeData, nil
}
