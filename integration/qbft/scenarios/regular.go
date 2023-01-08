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

		if actual == nil {
			return fmt.Errorf("expected actual = non-nil, actual = nil")
		}

		if actual.State == nil {
			return fmt.Errorf("expected state = non-nil, actual = nil")
		}

		if actual.State.Share.OperatorID != operatorID {
			return fmt.Errorf("expected share operator id = %d, actual = %d", operatorID, actual.State.Share.OperatorID)
		}

		if !bytes.Equal(actual.State.Share.ValidatorPubKey, spectestingutils.Testing4SharesSet().ValidatorPK.Serialize()) {
			return fmt.Errorf("expected share validator pub key %s, actual = %s", spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), actual.State.Share.ValidatorPubKey)
		}

		if !bytes.Equal(actual.State.Share.SharePubKey, spectestingutils.Testing4SharesSet().Shares[operatorID].GetPublicKey().Serialize()) {
			return fmt.Errorf("expected share share pub key %s, actual = %s", spectestingutils.Testing4SharesSet().Shares[operatorID].GetPublicKey().Serialize(), actual.State.Share.SharePubKey)
		}

		for i := range spectestingutils.Testing4SharesSet().Committee() {
			if actual.State.Share.Committee[i].OperatorID != spectestingutils.Testing4SharesSet().Committee()[i].OperatorID &&
				!bytes.Equal(actual.State.Share.Committee[i].PubKey, spectestingutils.Testing4SharesSet().Committee()[i].PubKey) {
				return fmt.Errorf("expected share comittee = %+v, actual = %+v", spectestingutils.Testing4SharesSet().Committee(), actual.State.Share.Committee)
			}
		}

		if actual.State.Share.Quorum != spectestingutils.Testing4SharesSet().Threshold {
			return fmt.Errorf("expected share quorum = %d, actual = %d", spectestingutils.Testing4SharesSet().Threshold, actual.State.Share.Quorum)
		}

		if actual.State.Share.PartialQuorum != spectestingutils.Testing4SharesSet().PartialThreshold {
			return fmt.Errorf("expected share partial quorum = %d, actual = %d", spectestingutils.Testing4SharesSet().PartialThreshold, actual.State.Share.PartialQuorum)
		}

		if !bytes.Equal(actual.State.Share.DomainType, spectypes.PrimusTestnet) {
			return fmt.Errorf("expected share domain type = %s, actual = %s", spectypes.PrimusTestnet, actual.State.Share.DomainType)
		}

		if !bytes.Equal(actual.State.Share.Graffiti, []byte{}) {
			return fmt.Errorf("expected share graffiti = %s, actual = %s", []byte{}, actual.State.Share.Graffiti)
		}

		if !bytes.Equal(actual.State.ID, identifier[:]) {
			return fmt.Errorf("expected id = %s, actual = %s", identifier[:], actual.State.ID)
		}

		if actual.State.Round != specqbft.FirstRound {
			return fmt.Errorf("expected round = %d, actual = %d", specqbft.FirstRound, actual.State.Round)
		}

		if actual.State.Height != specqbft.FirstHeight {
			return fmt.Errorf("expected height = %d, actual = %d", specqbft.FirstHeight, actual.State.Height)
		}

		if actual.State.LastPreparedRound != specqbft.FirstRound {
			return fmt.Errorf("expected last prepared round = %d, actual = %d", specqbft.FirstRound, actual.State.LastPreparedRound)
		}

		if !bytes.Equal(actual.State.LastPreparedValue, consensusData) {
			return fmt.Errorf("expected last prepared value = %s, actual = %s", consensusData, actual.State.LastPreparedValue)
		}

		proposalData, err := (&specqbft.ProposalData{
			Data:                     consensusData,
			RoundChangeJustification: nil,
			PrepareJustification:     nil,
		}).Encode()
		if err != nil {
			return fmt.Errorf("error during encoding specqbft.ProposalData: %w", err)
		}

		prepareData, err := (&specqbft.PrepareData{
			Data: consensusData,
		}).Encode()
		if err != nil {
			return fmt.Errorf("error during encoding specqbft.PrepareData: %w", err)
		}

		commitData, err := (&specqbft.CommitData{
			Data: consensusData,
		}).Encode()
		if err != nil {
			return fmt.Errorf("error during encoding specqbft.CommitData: %w", err)
		}

		expectedMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     specqbft.FirstHeight,
			Round:      specqbft.FirstRound,
			Identifier: identifier[:],
			Data:       proposalData,
		})
		if err := deepValidateSignedMessage(expectedMsg, actual.State.ProposalAcceptedForCurrentRound); err != nil {
			return fmt.Errorf("expected signed message = %+v, actual = %+v", expectedMsg, actual.State.ProposalAcceptedForCurrentRound)
		}

		if actual.State.Decided != true {
			return fmt.Errorf("expected decided = %v, actual = %v", true, actual.State.Decided)
		}

		if !bytes.Equal(actual.State.DecidedValue, consensusData) {
			return fmt.Errorf("expected decided value = %s, actual = %s", consensusData, actual.State.DecidedValue)
		}

		expectedMsg = spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
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
			operatorID := spectypes.OperatorID(i)

			expectedMsg = spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorID], operatorID, &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     specqbft.FirstHeight,
				Round:      specqbft.FirstRound,
				Identifier: identifier[:],
				Data:       prepareData,
			})
			if !isMessageExistInRound(expectedMsg, actual.State.PrepareContainer.Msgs[specqbft.FirstRound]) {
				return fmt.Errorf("prepare message %+v wasn't found at actual.State.PrepareContainer.Msgs[specqbft.FirstRound]", expectedMsg)
			}

			expectedMsg = spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[operatorID], operatorID, &specqbft.Message{
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

		if foundedMsgsCounter < 3 {
			return fmt.Errorf("wasn't found enough commit messages at actual.State.CommitContainer.Msgs[specqbft.FirstRound], expected at least 3, actual = %d", foundedMsgsCounter)
		}

		return nil
	}
}

func deepValidateSignedMessage(expected, actual *specqbft.SignedMessage) error {
	if !bytes.Equal(expected.Signature, actual.Signature) {
		return fmt.Errorf("expected signed message signature = %s, actual = %s", expected.Signature, actual.Signature)
	}

	for i := range expected.Signers {
		if expected.Signers[i] != actual.Signers[i] {
			return fmt.Errorf("expected signed message signer N%d operator id = %d, actual = %d", i, expected.Signers[i], actual.Signers[i])
		}
	}

	if expected.Message.MsgType != actual.Message.MsgType {
		return fmt.Errorf("expected signed message type = %d, actual = %d", expected.Message.MsgType, actual.Message.MsgType)
	}

	if expected.Message.Height != actual.Message.Height {
		return fmt.Errorf("expected signed message height = %d, actual = %d", expected.Message.Height, actual.Message.Height)
	}

	if expected.Message.Round != actual.Message.Round {
		return fmt.Errorf("expected signed message round = %d, actual = %d", expected.Message.Round, actual.Message.Round)
	}

	if !bytes.Equal(expected.Message.Identifier, actual.Message.Identifier) {
		return fmt.Errorf("expected signed message identifier = %s, actual = %s", expected.Message.Identifier, actual.Message.Identifier)
	}

	if !bytes.Equal(expected.Message.Data, actual.Message.Data) {
		return fmt.Errorf("expected signed message data = %s, actual = %s", expected.Message.Data, actual.Message.Data)
	}

	return nil
}

func isMessageExistInRound(message *specqbft.SignedMessage, round []*specqbft.SignedMessage) bool {
	for i := range round {
		if err := deepValidateSignedMessage(message, round[i]); err == nil {
			return true
		}
	}
	return false
}
