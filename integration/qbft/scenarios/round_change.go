package scenarios

import (
	"encoding/json"
	"fmt"
	"log"
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
		Duty: createDuty(pk, slots[3], 1, role),
		AttestationData: &spec.AttestationData{
			Slot:            slots[3],
			Index:           spectestingutils.TestingAttestationData.Index,
			BeaconBlockRoot: spectestingutils.TestingAttestationData.BeaconBlockRoot,
			Source:          spectestingutils.TestingAttestationData.Source,
			Target:          spectestingutils.TestingAttestationData.Target,
		},
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
		Identifier:       identifier,
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

		// sometimes there may be no prepare quorum TODO add quorum check after fixes
		_, prepareMessages := actual.State.PrepareContainer.LongestUniqueSignersForRoundAndValue(specqbft.FirstRound, prepareData)

		expectedPrepareMsg := &specqbft.SignedMessage{
			Message: &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     2,
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
				Height:     2,
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

		createPossibleState := func(lastPreparedRound specqbft.Round, lastPreparedValue []byte) *specqbft.State {
			return &specqbft.State{
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
			}
		}

		possibleStates := []*specqbft.State{
			createPossibleState(specqbft.FirstRound, consensusData),
			createPossibleState(0, nil),
		}

		var stateFound bool
		for _, state := range possibleStates {
			if err := validateByRoot(state, actual.State); err == nil {
				stateFound = true
				break
			}
		}

		if !stateFound {
			actualStateJSON, err := json.Marshal(actual.State)
			if err != nil {
				return fmt.Errorf("marshal actual state")
			}

			log.Printf("actual state: %v", string(actualStateJSON))
			return fmt.Errorf("state doesn't match any possible expected state")
		}

		expectedDecidedMessage := &specqbft.SignedMessage{
			Message: &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     2,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       spectestingutils.PrepareDataBytes(consensusData),
			},
		}

		if err := validateByRoot(expectedDecidedMessage, actual.DecidedMessage); err != nil {
			return err
		}

		return nil
	}
}
