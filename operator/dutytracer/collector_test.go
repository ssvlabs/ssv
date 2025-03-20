package validator

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

func TestValidatorDuty(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot         = phase0.Slot(1)
		role, bnRole = spectypes.RoleAggregator, spectypes.BNRoleAggregator
		vIndex       = phase0.ValidatorIndex(55)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("pk"), role)

	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	validators := registrystoragemocks.NewMockValidatorStore(ctrl)

	collector := New(context.TODO(), logger, validators, nil, nil, networkconfig.TestNetwork.Beacon.GetBeaconNetwork())

	var wantBeaconRoot phase0.Root
	bnVal := [32]byte{1, 2, 3}
	copy(wantBeaconRoot[:], bnVal[:])

	var validatorPK spectypes.ValidatorPK
	copy(validatorPK[:], identifier.GetDutyExecutorID()[:])

	fakeSig := [96]byte{}

	partialSigType := spectypes.VoluntaryExitPartialSig
	var partSigMessages = spectypes.PartialSignatureMessages{
		Type: partialSigType,
		Slot: 1,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				ValidatorIndex:   vIndex,
				Signer:           99,
				PartialSignature: fakeSig[:],
				SigningRoot:      [32]byte{1, 2, 3},
			},
		},
	}

	partSigMessagesData, err := partSigMessages.Encode()
	require.NoError(t, err)

	dummyVerify := func(*spectypes.PartialSignatureMessages) error { return nil }
	{ // TC 1 - PartialSig - Aggregator - pre-consensus
		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		collector.Collect(context.Background(), partSigMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)
		assert.Len(t, duty.Pre, 1)

		pre := duty.Pre[0]
		assert.Equal(t, partialSigType, pre.Type)
		assert.Equal(t, wantBeaconRoot, pre.BeaconRoot)
		assert.Equal(t, uint64(99), pre.Signer)
		assert.NotNil(t, pre.ReceivedTime)
	}

	partialSigType = spectypes.PostConsensusPartialSig
	partSigMessages.Type = partialSigType
	partSigMessagesData, err = partSigMessages.Encode()
	require.NoError(t, err)

	{ // TC 2 - PartialSig - Aggregator - post-consensus
		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		collector.Collect(context.Background(), partSigMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)
		assert.Len(t, duty.Post, 1)

		post := duty.Post[0]
		assert.Equal(t, partialSigType, post.Type)
		assert.Equal(t, wantBeaconRoot, post.BeaconRoot)
		assert.Equal(t, uint64(99), post.Signer)
		assert.NotNil(t, post.ReceivedTime)

		require.NotNil(t, duty.ConsensusTrace)
		require.Empty(t, duty.Decideds)
		require.Empty(t, duty.Rounds)
	}

	// consensus

	{ // TC - 3 - Proposal
		proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		collector.Collect(context.Background(), proposalMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 1)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)
		require.Len(t, duty.Rounds, 1)

		// require.Equal(t, uint64(1), round.Proposer) // populate proposer?
		require.NotNil(t, round.ProposalTrace)
		require.NotNil(t, round.ProposalTrace.QBFTTrace)

		qbtf := round.ProposalTrace.QBFTTrace
		assert.Equal(t, uint64(1), qbtf.Round)
		assert.Equal(t, wantBeaconRoot, qbtf.BeaconRoot)
		assert.Equal(t, uint64(1), qbtf.Signer)
		require.NotNil(t, qbtf.ReceivedTime)

		require.Len(t, round.ProposalTrace.RoundChanges, 1)
		rc := round.ProposalTrace.RoundChanges[0]
		assert.Equal(t, uint64(1), rc.Round)
		assert.Equal(t, wantBeaconRoot, rc.BeaconRoot)
		assert.Equal(t, uint64(1), rc.Signer)
		assert.Equal(t, uint64(1), rc.PreparedRound)

		assert.Len(t, rc.PrepareMessages, 1)
		pm := rc.PrepareMessages[0]
		assert.Equal(t, uint64(1), pm.Round)
		assert.Equal(t, wantBeaconRoot, pm.BeaconRoot)
		assert.Equal(t, uint64(1), pm.Signer)
		require.NotNil(t, pm.ReceivedTime)

		require.NotNil(t, rc.ReceivedTime)

		require.Empty(t, duty.Decideds)
	}

	{ // TC - 4 - Prepare
		prepareMsg := buildConsensusMsg(identifier, specqbft.PrepareMsgType, slot, nil)
		collector.Collect(context.Background(), prepareMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 1)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)
		require.Len(t, round.Prepares, 1)

		prepare := round.Prepares[0]
		require.NotNil(t, prepare)
		assert.Equal(t, uint64(1), prepare.Round)
		assert.Equal(t, wantBeaconRoot, prepare.BeaconRoot)
		assert.Equal(t, uint64(1), prepare.Signer)
		require.NotNil(t, prepare.ReceivedTime)
	}

	{ // TC - 5 - Decided
		decidedMsg := buildConsensusMsg(identifier, specqbft.CommitMsgType, slot, nil)
		collector.Collect(context.Background(), decidedMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 1)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)

		require.Len(t, duty.Decideds, 1)

		decided := duty.Decideds[0]
		require.NotNil(t, decided)
		assert.Equal(t, uint64(1), decided.Round)
		assert.Equal(t, wantBeaconRoot, decided.BeaconRoot)
		require.NotNil(t, decided.ReceivedTime)

		require.Empty(t, round.Commits)
		require.Empty(t, round.RoundChanges)
	}

	{ // TC - 6 - Commit
		commitMsg := buildConsensusMsg(identifier, specqbft.CommitMsgType, slot, nil)
		commitMsg.SignedSSVMessage.OperatorIDs = []spectypes.OperatorID{1}
		collector.Collect(context.Background(), commitMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 1)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)

		require.Len(t, round.Commits, 1)

		commit := round.Commits[0]
		require.NotNil(t, commit)
		assert.Equal(t, uint64(1), commit.Round)
		assert.Equal(t, wantBeaconRoot, commit.BeaconRoot)
		require.NotNil(t, commit.ReceivedTime)
	}

	{ // TC - 7 - RoundChange
		roundChangeMsg := buildConsensusMsg(identifier, specqbft.RoundChangeMsgType, slot, nil)
		collector.Collect(context.Background(), roundChangeMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 1)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)
		require.NotNil(t, round.RoundChanges)
		require.Len(t, round.RoundChanges, 1)

		roundChange := round.RoundChanges[0]
		require.NotNil(t, roundChange)
		assert.Equal(t, uint64(1), roundChange.Round)
		assert.Equal(t, wantBeaconRoot, roundChange.BeaconRoot)
		assert.Equal(t, uint64(1), roundChange.Signer)
		require.NotNil(t, roundChange.ReceivedTime)
	}

	{ // TC - 8 - Second RoundChange
		roundChangeMsg := buildConsensusMsg(identifier, specqbft.RoundChangeMsgType, slot, nil)
		roundChangeMsg.Body.(*specqbft.Message).Round = 2
		collector.Collect(context.Background(), roundChangeMsg, dummyVerify)

		duty, err := collector.GetValidatorDuties(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.ConsensusTrace.Rounds, 2)

		round := duty.ConsensusTrace.Rounds[0]
		require.NotNil(t, round)
		require.NotNil(t, round.RoundChanges)
		require.Len(t, round.RoundChanges, 1)

		roundChange := round.RoundChanges[0]
		require.NotNil(t, roundChange)
		assert.Equal(t, uint64(1), roundChange.Round)
		assert.Equal(t, wantBeaconRoot, roundChange.BeaconRoot)
		assert.Equal(t, uint64(1), roundChange.Signer)
		require.NotNil(t, roundChange.ReceivedTime)
	}
}

func TestCommitteeDuty(t *testing.T) {
	logger := zap.NewNop()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		slot   = phase0.Slot(1)
		vIndex = phase0.ValidatorIndex(55)
	)

	identifier := spectypes.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleCommittee)

	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	committee := &storage.Committee{
		ID:        committeeID,
		Operators: []spectypes.OperatorID{1, 2, 3, 4},
	}
	validators := registrystoragemocks.NewMockValidatorStore(ctrl)
	validators.EXPECT().Committee(committeeID).Return(committee, true)

	tracer := New(context.TODO(), logger, validators, nil, nil, networkconfig.TestNetwork.Beacon.GetBeaconNetwork())

	var wantBeaconRoot phase0.Root
	bnVal := [32]byte{1, 2, 3}
	copy(wantBeaconRoot[:], bnVal[:])

	{ // TC 1 - process partial sig messages
		fakeSig := [96]byte{}

		var partSigMessages = spectypes.PartialSignatureMessages{
			Slot: 1,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					ValidatorIndex:   vIndex,
					Signer:           99,
					PartialSignature: fakeSig[:],
					SigningRoot:      [32]byte{1, 2, 3},
				},
			},
		}

		partSigMessagesData, err := partSigMessages.Encode()
		require.NoError(t, err)

		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		tracer.Collect(context.Background(), partSigMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.NotNil(t, duty.Attester)
		require.NotEmpty(t, duty.Attester)

		attester := duty.Attester[0]
		require.NotEmpty(t, attester.Signers)
		assert.Equal(t, uint64(99), attester.Signers[0])

		require.NotNil(t, duty.ConsensusTrace)
		require.Empty(t, duty.Decideds)
		require.Empty(t, duty.Rounds)
		require.Empty(t, duty.SyncCommittee)
		require.Equal(t, committee.Operators, duty.OperatorIDs)

		// slotToCommittee, found := tracer.validatorIndexToCommitteeMapping.Load(vIndex)
		// require.True(t, found)
		// committeeID, found := slotToCommittee.Load(slot)
		// require.True(t, found)
		assert.Equal(t, committeeID, committeeID)
	}

	{ // TC 2 - Proposal
		proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		tracer.Collect(context.Background(), proposalMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.Len(t, duty.Rounds, 1)
		round0 := duty.Rounds[0]
		require.NotNil(t, round0)
		require.NotNil(t, round0.ProposalTrace)
		require.NotNil(t, round0.ProposalTrace.QBFTTrace)
		assert.Equal(t, uint64(1), round0.ProposalTrace.QBFTTrace.Round)

		assert.Equal(t, wantBeaconRoot, round0.ProposalTrace.QBFTTrace.BeaconRoot)
		assert.Equal(t, uint64(1), round0.ProposalTrace.QBFTTrace.Signer)
		require.NotNil(t, round0.ProposalTrace.ReceivedTime)

		require.Empty(t, round0.Prepares)
		require.Empty(t, round0.Commits)
		require.Empty(t, round0.RoundChanges)
	}

	{ // TC 3 - Prepare
		prepareMsg := buildConsensusMsg(identifier, specqbft.PrepareMsgType, slot, nil)
		tracer.Collect(context.Background(), prepareMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.Len(t, duty.Rounds, 1)
		round0 := duty.Rounds[0]
		require.NotNil(t, round0)
		require.Len(t, round0.Prepares, 1)

		prepare0 := round0.Prepares[0]
		require.NotNil(t, prepare0)
		assert.Equal(t, uint64(1), prepare0.Round)
		assert.Equal(t, wantBeaconRoot, prepare0.BeaconRoot)
		assert.Equal(t, uint64(1), prepare0.Signer)
		require.NotNil(t, prepare0.ReceivedTime)

		require.Empty(t, round0.Commits)
		require.Empty(t, round0.RoundChanges)
	}

	{ // TC 4 - Decided
		decided := buildConsensusMsg(identifier, specqbft.CommitMsgType, slot, generateDecidedMessage(t, identifier))
		tracer.Collect(context.Background(), decided, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.NotNil(t, duty.Attester)
		require.NotEmpty(t, duty.Attester)

		attester := duty.Attester[0]
		require.NotEmpty(t, attester.Signers)
		assert.Equal(t, uint64(99), attester.Signers[0])

		require.NotNil(t, duty.ConsensusTrace)
		require.Empty(t, duty.SyncCommittee)
		require.Equal(t, committee.Operators, duty.OperatorIDs)

		require.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
		require.NotNil(t, round)
		// assert.Equal(t, uint64(2), round.Proposer)

		require.Empty(t, round.Commits)
		require.Empty(t, round.RoundChanges)

		decideds := duty.Decideds
		require.Len(t, decideds, 1)

		decided0 := decideds[0]
		assert.Equal(t, wantBeaconRoot, decided0.BeaconRoot)
		assert.Equal(t, decided0.Signers, committee.Operators)
		assert.Equal(t, decided0.Round, uint64(1))
	}

	{ // TC 5 - Commit
		commitMsg := buildConsensusMsg(identifier, specqbft.CommitMsgType, slot, nil)
		commitMsg.SignedSSVMessage.OperatorIDs = []spectypes.OperatorID{1}

		tracer.Collect(context.Background(), commitMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.Len(t, duty.Rounds, 1)
		round0 := duty.Rounds[0]
		require.NotNil(t, round0)
		require.Len(t, round0.Commits, 1)

		commit0 := round0.Commits[0]
		require.NotNil(t, commit0)
		assert.Equal(t, uint64(1), commit0.Round)
		assert.Equal(t, wantBeaconRoot, commit0.BeaconRoot)
		assert.Equal(t, uint64(1), commit0.Signer)
		require.NotNil(t, commit0.ReceivedTime)

		require.Empty(t, round0.RoundChanges)
		require.Len(t, round0.Prepares, 1)
		require.Len(t, round0.Commits, 1)
	}

	{ // TC 6 - RoundChange
		roundChangeMsg1 := buildConsensusMsg(identifier, specqbft.RoundChangeMsgType, slot, nil)
		tracer.Collect(context.Background(), roundChangeMsg1, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.Len(t, duty.Rounds, 1)
		round0 := duty.Rounds[0]
		require.NotNil(t, round0)
		require.Len(t, round0.RoundChanges, 1)

		roundChange0 := round0.RoundChanges[0]
		require.NotNil(t, roundChange0)
		assert.Equal(t, uint64(1), roundChange0.Round)
		assert.Equal(t, wantBeaconRoot, roundChange0.BeaconRoot)
		assert.Equal(t, uint64(1), roundChange0.Signer)
		require.NotNil(t, roundChange0.ReceivedTime)

		require.Len(t, round0.Prepares, 1)
		require.Len(t, round0.Commits, 1)
	}

	{ // TC 7 - Second RoundChange
		roundChangeMsg2 := buildConsensusMsg(identifier, specqbft.RoundChangeMsgType, slot, nil)
		roundChangeMsg2.Body.(*specqbft.Message).Round = 2

		tracer.Collect(context.Background(), roundChangeMsg2, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)
		require.Len(t, duty.Rounds, 2)
		round1 := duty.Rounds[1]
		require.NotNil(t, round1)
		// assert.Equal(t, uint64(3), round1.Proposer)
		require.Len(t, round1.RoundChanges, 1)

		roundChange := round1.RoundChanges[0]
		require.NotNil(t, roundChange)
		assert.Equal(t, uint64(2), roundChange.Round)
		assert.Equal(t, wantBeaconRoot, roundChange.BeaconRoot)
		assert.Equal(t, uint64(1), roundChange.Signer)
		require.NotNil(t, roundChange.ReceivedTime)

		require.Nil(t, round1.ProposalTrace)
		require.Empty(t, round1.Prepares)
		require.Empty(t, round1.Commits)
	}
}

func buildPartialSigMessage(identifier spectypes.MessageID, data []byte) *queue.SSVMessage {
	return &queue.SSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgID:   identifier,
			MsgType: spectypes.SSVPartialSignatureMsgType,
		},
		SignedSSVMessage: &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVPartialSignatureMsgType,
				Data:    data,
			},
		},
	}
}

func buildConsensusMsg(identifier spectypes.MessageID, msgType specqbft.MessageType, slot phase0.Slot, data []byte) *queue.SSVMessage {
	return &queue.SSVMessage{
		SignedSSVMessage: &spectypes.SignedSSVMessage{
			OperatorIDs: []spectypes.OperatorID{1, 2, 3, 4},
		},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   identifier,
			Data:    data,
		},
		Body: &specqbft.Message{
			MsgType:                  msgType,
			Height:                   specqbft.Height(slot),
			Round:                    1,
			DataRound:                1,
			Identifier:               identifier[:],
			Root:                     [32]byte{1, 2, 3},
			RoundChangeJustification: [][]byte{justification([][]byte{justification(nil)})},
			PrepareJustification:     [][]byte{justification([][]byte{justification(nil)})},
		},
	}
}

func justification(rcj [][]byte) []byte {
	qmsg := &specqbft.Message{
		MsgType:                  specqbft.ProposalMsgType,
		Round:                    1,
		DataRound:                1,
		Height:                   1,
		Root:                     [32]byte{1, 2, 3},
		RoundChangeJustification: rcj,
	}

	qmsgData, _ := qmsg.Encode()

	m := &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{1, 2, 3, 4},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			Data:    qmsgData,
		},
	}

	data, _ := m.Encode()

	return data
}

func TestDutyTracer_SyncCommitteeRoots(t *testing.T) {
	collector := New(context.TODO(), zap.NewNop(), nil, mockclient{}, nil, networkconfig.TestNetwork.Beacon.GetBeaconNetwork())

	bnVote := &spectypes.BeaconVote{BlockRoot: [32]byte{1, 2, 3}}

	data, _ := bnVote.Encode()
	root, err := collector.getSyncCommitteeRoot(1, data)
	require.NoError(t, err)

	wantRoot := [32]byte{3, 73, 222, 196, 134, 206, 159, 128,
		166, 167, 30, 61, 93, 176, 31, 245, 206, 128, 55, 43,
		252, 38, 103, 222, 41, 238, 156, 242, 86, 60, 152, 240}
	assert.Equal(t, phase0.Root(wantRoot), root)
}

type mockclient struct{}

func (m mockclient) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, nil
}

func generateDecidedMessage(t *testing.T, identifier spectypes.MessageID) []byte {
	msg := specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     0,
		Round:      1,
		Identifier: identifier[:],
		Root:       [32]byte{1, 2, 3},
	}
	msgEncoded, err := msg.Encode()
	if err != nil {
		panic(err)
	}
	sig := append([]byte{1, 2, 3, 4}, make([]byte, 92)...)
	sm := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID(msg.Identifier),
			Data:    msgEncoded,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  append(make([][]byte, 0), sig),
		OperatorIDs: []spectypes.OperatorID{1, 2, 3},
	}
	res, err := sm.Encode()
	require.NoError(t, err)
	return res
}
