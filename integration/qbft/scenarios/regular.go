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
		Duty:                      createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, spectypes.BNRoleAttester)[0],
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
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				storedInstanceForOperatorID(1, identifier, consensusData),
			},
			2: {
				storedInstanceForOperatorID(2, identifier, consensusData),
			},
			3: {
				storedInstanceForOperatorID(3, identifier, consensusData),
			},
			4: {
				storedInstanceForOperatorID(4, identifier, consensusData),
			},
		},
		InstanceValidators: map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error{
			1: {
				func(actual *protocolstorage.StoredInstance) error {
					consensusData, err := consensusData.Encode()
					if err != nil {
						panic(err)
					}

					if actual.State == nil {
						return fmt.Errorf("expected state = non-nil, actual = nil")
					}

					if actual.State.Share.OperatorID != 1 {
						return fmt.Errorf("expected share operator id = %d, actual = %d", 1, actual.State.Share.OperatorID)
					}

					if !bytes.Equal(actual.State.Share.ValidatorPubKey, spectestingutils.Testing4SharesSet().ValidatorPK.Serialize()) {
						return fmt.Errorf("expected share validator pub key %s, actual = %s", spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), actual.State.Share.ValidatorPubKey)
					}

					if !bytes.Equal(actual.State.Share.SharePubKey, spectestingutils.Testing4SharesSet().Shares[1].GetPublicKey().Serialize()) {
						return fmt.Errorf("expected share share pub key %s, actual = %s", spectestingutils.Testing4SharesSet().Shares[1].GetPublicKey().Serialize(), actual.State.Share.SharePubKey)
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

					expectedMsg := spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
						MsgType:    specqbft.ProposalMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       proposalData,
					})
					if !bytes.Equal(actual.State.ProposalAcceptedForCurrentRound.Signature, expectedMsg.Signature) {
						return fmt.Errorf("expected signed message signature = %s, actual = %s", expectedMsg.Signature, actual.State.ProposalAcceptedForCurrentRound.Signature)
					}

					for i := range expectedMsg.Signers {
						if actual.State.ProposalAcceptedForCurrentRound.Signers[i] != expectedMsg.Signers[i] {
							return fmt.Errorf("expected signed message signer N%d operator id = %d, actual = %d", i, expectedMsg.Signers[i], actual.State.ProposalAcceptedForCurrentRound.Signers[i])
						}
					}

					if actual.State.ProposalAcceptedForCurrentRound.Message.MsgType != expectedMsg.Message.MsgType {
						return fmt.Errorf("expected signed message type = %d, actual = %d", expectedMsg.Message.MsgType, actual.State.ProposalAcceptedForCurrentRound.Message.MsgType)
					}

					if actual.State.ProposalAcceptedForCurrentRound.Message.Height != expectedMsg.Message.Height {
						return fmt.Errorf("expected signed message height = %d, actual = %d", expectedMsg.Message.Height, actual.State.ProposalAcceptedForCurrentRound.Message.Height)
					}

					if actual.State.ProposalAcceptedForCurrentRound.Message.Round != expectedMsg.Message.Round {
						return fmt.Errorf("expected signed message round = %d, actual = %d", expectedMsg.Message.Round, actual.State.ProposalAcceptedForCurrentRound.Message.Round)
					}

					if !bytes.Equal(actual.State.ProposalAcceptedForCurrentRound.Message.Identifier, expectedMsg.Message.Identifier) {
						return fmt.Errorf("expected signed message identifier = %s, actual = %s", expectedMsg.Message.Identifier, actual.State.ProposalAcceptedForCurrentRound.Message.Identifier)
					}

					if !bytes.Equal(actual.State.ProposalAcceptedForCurrentRound.Message.Data, expectedMsg.Message.Data) {
						return fmt.Errorf("expected signed message data = %s, actual = %s", expectedMsg.Message.Data, actual.State.ProposalAcceptedForCurrentRound.Message.Data)
					}

					if actual.State.Decided != true {
						return fmt.Errorf("expected decided = %v, actual = %v", true, actual.State.Decided)
					}

					if !bytes.Equal(actual.State.DecidedValue, consensusData) {
						return fmt.Errorf("expected decided value = %s, actual = %s", consensusData, actual.State.DecidedValue)
					}

					return nil
				},
			},
			2: {
				func(actual *protocolstorage.StoredInstance) error {
					expected := storedInstanceForOperatorID(2, identifier, consensusData)

					return deepValidateStorageInstance(expected, actual)
				},
			},
			3: {
				func(actual *protocolstorage.StoredInstance) error {
					expected := storedInstanceForOperatorID(3, identifier, consensusData)

					return deepValidateStorageInstance(expected, actual)
				},
			},
			4: {
				func(actual *protocolstorage.StoredInstance) error {
					expected := storedInstanceForOperatorID(4, identifier, consensusData)

					return deepValidateStorageInstance(expected, actual)
				},
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

func storedInstanceForOperatorID(operatorID spectypes.OperatorID, identifier spectypes.MessageID, data *spectypes.ConsensusData) *protocolstorage.StoredInstance {
	consensusData, err := data.Encode()
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

	return &protocolstorage.StoredInstance{
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
			ProposeContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
				specqbft.FirstRound: {
					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
						MsgType:    specqbft.ProposalMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       proposalData,
					}),
				},
			}},
			PrepareContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
				specqbft.FirstRound: {
					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[1], 1, &specqbft.Message{
						MsgType:    specqbft.PrepareMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       prepareData,
					}),
					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[2], 2, &specqbft.Message{
						MsgType:    specqbft.PrepareMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       prepareData,
					}),
					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[3], 3, &specqbft.Message{
						MsgType:    specqbft.PrepareMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       prepareData,
					}),
					spectestingutils.SignQBFTMsg(spectestingutils.Testing4SharesSet().Shares[4], 4, &specqbft.Message{
						MsgType:    specqbft.PrepareMsgType,
						Height:     specqbft.FirstHeight,
						Round:      specqbft.FirstRound,
						Identifier: identifier[:],
						Data:       prepareData,
					}),
				},
			}},
			// NOTE: need to keep round keys and commit message data, there's a special check for CommitContainer
			CommitContainer: &specqbft.MsgContainer{Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
				specqbft.FirstRound: {
					&specqbft.SignedMessage{
						Message: &specqbft.Message{
							Data: commitData,
						},
					},
				},
			}},
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
}

func deepValidateStorageInstance(expected, actual *protocolstorage.StoredInstance) error {
	if actual.State == nil {
		return fmt.Errorf("expected state = non-nil, actual = nil")
	}

	if expected.State.Share.OperatorID != actual.State.Share.OperatorID {
		return fmt.Errorf("expected share operator id = %d, actual = %d", expected.State.Share.OperatorID, actual.State.Share.OperatorID)
	}

	if !bytes.Equal(expected.State.Share.ValidatorPubKey, actual.State.Share.ValidatorPubKey) {
		return fmt.Errorf("expected share validator pub key %s, actual = %s", expected.State.Share.ValidatorPubKey, actual.State.Share.ValidatorPubKey)
	}

	if !bytes.Equal(expected.State.Share.SharePubKey, actual.State.Share.SharePubKey) {
		return fmt.Errorf("expected share share pub key %s, actual = %s", expected.State.Share.SharePubKey, actual.State.Share.SharePubKey)
	}

	for i := range expected.State.Share.Committee {
		if expected.State.Share.Committee[i].OperatorID != actual.State.Share.Committee[i].OperatorID &&
			!bytes.Equal(expected.State.Share.Committee[i].PubKey, actual.State.Share.Committee[i].PubKey) {
			return fmt.Errorf("expected share comittee = %+v, actual = %+v", expected.State.Share.Committee, actual.State.Share.Committee)
		}
	}

	if expected.State.Share.Quorum != actual.State.Share.Quorum {
		return fmt.Errorf("expected share quorum = %d, actual = %d", expected.State.Share.Quorum, actual.State.Share.Quorum)
	}

	if expected.State.Share.PartialQuorum != actual.State.Share.PartialQuorum {
		return fmt.Errorf("expected share partial quorum = %d, actual = %d", expected.State.Share.PartialQuorum, actual.State.Share.PartialQuorum)
	}

	if !bytes.Equal(expected.State.Share.DomainType, actual.State.Share.DomainType) {
		return fmt.Errorf("expected share domain type = %d, actual = %d", expected.State.Share.DomainType, actual.State.Share.DomainType)
	}

	if !bytes.Equal(expected.State.Share.Graffiti, actual.State.Share.Graffiti) {
		return fmt.Errorf("expected share graffiti id = %d, actual = %d", expected.State.Share.Graffiti, actual.State.Share.Graffiti)
	}

	if !bytes.Equal(expected.State.ID, actual.State.ID) {
		return fmt.Errorf("expected id = %s, actual = %s", expected.State.ID, actual.State.ID)
	}

	if expected.State.Round != actual.State.Round {
		return fmt.Errorf("expected round = %d, actual = %d", expected.State.Round, actual.State.Round)
	}

	if expected.State.Height != actual.State.Height {
		return fmt.Errorf("expected height = %d, actual = %d", expected.State.Height, actual.State.Height)
	}

	if expected.State.LastPreparedRound != actual.State.LastPreparedRound {
		return fmt.Errorf("expected last prepared round = %d, actual = %d", expected.State.LastPreparedRound, actual.State.LastPreparedRound)
	}

	if !bytes.Equal(expected.State.LastPreparedValue, actual.State.LastPreparedValue) {
		return fmt.Errorf("expected last prepared value = %s, actual = %s", expected.State.LastPreparedValue, actual.State.LastPreparedValue)
	}

	if err := deepValidateSignedMessage(expected.State.ProposalAcceptedForCurrentRound, actual.State.ProposalAcceptedForCurrentRound); err != nil {
		return err
	}

	if expected.State.Decided != actual.State.Decided {
		return fmt.Errorf("expected decided = %v, actual = %v", expected.State.Decided, actual.State.Decided)
	}

	if !bytes.Equal(expected.State.DecidedValue, actual.State.DecidedValue) {
		return fmt.Errorf("expected decided value = %s, actual = %s", expected.State.DecidedValue, actual.State.DecidedValue)
	}

	for range expected.State.ProposeContainer.Msgs[expected.State.Round] {
		for i := range expected.State.ProposeContainer.Msgs[expected.State.Round] {
			if err := deepValidateSignedMessage(expected.State.ProposeContainer.Msgs[expected.State.Round][i], actual.State.ProposeContainer.Msgs[actual.State.Round][i]); err != nil {
				return err
			}
		}
	}

	for range expected.State.PrepareContainer.Msgs[expected.State.Round] {
		for i := range expected.State.PrepareContainer.Msgs[expected.State.Round] {
			if err := deepValidateSignedMessage(expected.State.PrepareContainer.Msgs[expected.State.Round][i], actual.State.PrepareContainer.Msgs[actual.State.Round][i]); err != nil {
				return err
			}
		}
	}

	for range expected.State.CommitContainer.Msgs[expected.State.Round] {
		for i := range expected.State.CommitContainer.Msgs[expected.State.Round] {
			if err := deepValidateSignedMessage(expected.State.CommitContainer.Msgs[expected.State.Round][i], actual.State.CommitContainer.Msgs[actual.State.Round][i]); err != nil {
				return err
			}
		}
	}

	for range expected.State.RoundChangeContainer.Msgs[expected.State.Round] {
		for i := range expected.State.RoundChangeContainer.Msgs[expected.State.Round] {
			if err := deepValidateSignedMessage(expected.State.RoundChangeContainer.Msgs[expected.State.Round][i], actual.State.RoundChangeContainer.Msgs[actual.State.Round][i]); err != nil {
				return err
			}
		}
	}

	if err := deepValidateSignedMessage(expected.DecidedMessage, actual.DecidedMessage); err != nil {
		return err
	}

	return nil
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
