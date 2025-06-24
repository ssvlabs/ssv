package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// These tests are deliberately written in a progressive manner to ensure that only the expected values are
// changing at any given iteration.

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

	collector := New(t.Context(), logger, validators, nil, nil, networkconfig.TestNetwork.BeaconConfig)

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

	{ // TC 1 - PartialSig - Aggregator - pre-consensus
		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		err := collector.Collect(t.Context(), partSigMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
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
		err := collector.Collect(t.Context(), partSigMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
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
		err := collector.Collect(t.Context(), proposalMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
		require.NotNil(t, round)
		require.Len(t, duty.Rounds, 1)

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
		err := collector.Collect(t.Context(), prepareMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
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
		err := collector.Collect(t.Context(), decidedMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
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
		err := collector.Collect(t.Context(), commitMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
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
		err := collector.Collect(t.Context(), roundChangeMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 1)

		round := duty.Rounds[0]
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
		err := collector.Collect(t.Context(), roundChangeMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 2)

		round := duty.Rounds[0]
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

	{ // TC - 9 Proposal with proposal data
		proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		data, err := new(specqbft.Message).Encode()
		require.NoError(t, err)

		proposalMsg.Data = data

		pData, err := new(spectypes.ValidatorConsensusData).Encode()
		require.NoError(t, err)

		proposalMsg.SignedSSVMessage.FullData = pData

		err = collector.Collect(t.Context(), proposalMsg, dummyVerify)
		require.NoError(t, err)

		duty, err := collector.GetValidatorDuty(bnRole, slot, validatorPK)
		require.NoError(t, err)
		require.NotNil(t, duty)

		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, bnRole, duty.Role)
		assert.Equal(t, vIndex, duty.Validator)

		require.NotNil(t, duty.ConsensusTrace)
		assert.Len(t, duty.Rounds, 2)

		round := duty.Rounds[0]
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

	dutyStore := new(mockDutyTraceStore)
	tracer := New(t.Context(), logger, validators, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

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
		tracer.Collect(t.Context(), partSigMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.NotNil(t, duty.Attester)
		require.NotEmpty(t, duty.Attester)

		attester := duty.Attester[0]
		require.NotEmpty(t, attester.Signer)
		assert.Equal(t, uint64(99), attester.Signer)

		require.NotNil(t, duty.ConsensusTrace)
		require.Empty(t, duty.Decideds)
		require.Empty(t, duty.Rounds)
		require.Empty(t, duty.SyncCommittee)
		require.Equal(t, committee.Operators, duty.OperatorIDs)
		assert.Equal(t, committeeID, committeeID)
	}

	validators.EXPECT().Committee(committeeID).Return(committee, true)

	{ // TC 1b - process partial sig messages with sc root
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

		trace, _, err := tracer.getOrCreateCommitteeTrace(slot, committeeID)
		require.NoError(t, err)
		trace.syncCommitteeRoot = [32]byte{1, 2, 3}

		partSigMsg := buildPartialSigMessage(identifier, partSigMessagesData)
		tracer.Collect(t.Context(), partSigMsg, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.NotNil(t, duty.SyncCommittee)
		require.NotEmpty(t, duty.SyncCommittee)

		require.NotNil(t, duty.ConsensusTrace)
		require.Empty(t, duty.Decideds)
		require.Empty(t, duty.Rounds)
		require.Equal(t, committee.Operators, duty.OperatorIDs)
		assert.Equal(t, committeeID, committeeID)
	}

	{ // TC 2 - Proposal
		proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		tracer.Collect(t.Context(), proposalMsg, dummyVerify)

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
		assert.Equal(t, uint64(1), round0.ProposalTrace.Round)

		assert.Equal(t, wantBeaconRoot, round0.ProposalTrace.BeaconRoot)
		assert.Equal(t, uint64(1), round0.ProposalTrace.Signer)
		require.NotNil(t, round0.ProposalTrace.ReceivedTime)

		require.Empty(t, round0.Prepares)
		require.Empty(t, round0.Commits)
		require.Empty(t, round0.RoundChanges)
	}

	{ // TC 3 - Prepare
		prepareMsg := buildConsensusMsg(identifier, specqbft.PrepareMsgType, slot, nil)
		tracer.Collect(t.Context(), prepareMsg, dummyVerify)

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
		tracer.Collect(t.Context(), decided, dummyVerify)

		duty, err := tracer.GetCommitteeDuty(slot, committeeID)
		require.NoError(t, err)
		require.NotNil(t, duty)
		assert.Equal(t, slot, duty.Slot)
		assert.Equal(t, duty.CommitteeID, committeeID)

		require.NotNil(t, duty.Attester)
		require.NotEmpty(t, duty.Attester)

		attester := duty.Attester[0]
		require.NotEmpty(t, attester.Signer)
		assert.Equal(t, uint64(99), attester.Signer)

		require.NotNil(t, duty.ConsensusTrace)
		require.NotEmpty(t, duty.SyncCommittee)
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

		tracer.Collect(t.Context(), commitMsg, dummyVerify)

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
	}

	{ // TC 6 - RoundChange
		roundChangeMsg1 := buildConsensusMsg(identifier, specqbft.RoundChangeMsgType, slot, nil)
		tracer.Collect(t.Context(), roundChangeMsg1, dummyVerify)

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

		tracer.Collect(t.Context(), roundChangeMsg2, dummyVerify)

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

	{ // TC 8 - Proposal with proposal data
		proposalMsg := buildConsensusMsg(identifier, specqbft.ProposalMsgType, slot, nil)
		proposalMsg.SignedSSVMessage.FullData = []byte{1, 2, 3, 4}

		tracer.Collect(t.Context(), proposalMsg, dummyVerify)

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

	duties, err := tracer.GetCommitteeDuties(slot, spectypes.BNRoleAttester)
	require.NoError(t, err)
	require.NotNil(t, duties)
	require.Len(t, duties, 1)
	require.Equal(t, slot, duties[0].Slot)
	require.Equal(t, committeeID, duties[0].CommitteeID)
}

func TestCollector_GetCommitteeDuty(t *testing.T) {
	ctrl := gomock.NewController(t)
	vstore := registrystoragemocks.NewMockValidatorStore(ctrl)
	dutyStore := new(mockDutyTraceStore)
	dutyStore.committeeDutyTrace = &model.CommitteeDutyTrace{
		Slot:        phase0.Slot(10),
		CommitteeID: spectypes.CommitteeID{1},
		OperatorIDs: []uint64{1, 2, 3, 4},
	}

	collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
	committeeID := spectypes.CommitteeID{1}
	slot := phase0.Slot(10)

	_, err := collector.GetCommitteeDuty(slot, committeeID, spectypes.BNRoleAttester)
	require.ErrorIs(t, err, ErrNotFound)
	dutyStore.committeeDutyTrace.Attester = append(dutyStore.committeeDutyTrace.Attester,
		&model.SignerData{
			Signer: 1,
		})

	duty, err := collector.GetCommitteeDuty(slot, committeeID, spectypes.BNRoleAttester)
	require.NoError(t, err)
	require.NotNil(t, duty)
	require.Equal(t, slot, duty.Slot)
	require.Equal(t, committeeID, duty.CommitteeID)

	dutyStore.committeeDutyTrace.SyncCommittee = append(dutyStore.committeeDutyTrace.SyncCommittee,
		&model.SignerData{
			Signer: 1,
		})

	duty, err = collector.GetCommitteeDuty(slot, committeeID, spectypes.BNRoleSyncCommittee)
	require.NoError(t, err)
	require.NotNil(t, duty)
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
	collector := New(t.Context(), zap.NewNop(), nil, mockclient{}, nil, networkconfig.TestNetwork.BeaconConfig)

	bnVote := &spectypes.BeaconVote{BlockRoot: [32]byte{1, 2, 3}}

	data, _ := bnVote.Encode()
	root, err := collector.getSyncCommitteeRoot(t.Context(), 1, data)
	require.NoError(t, err)

	wantRoot := [32]byte{3, 73, 222, 196, 134, 206, 159, 128,
		166, 167, 30, 61, 93, 176, 31, 245, 206, 128, 55, 43,
		252, 38, 103, 222, 41, 238, 156, 242, 86, 60, 152, 240}
	assert.Equal(t, phase0.Root(wantRoot), root)
}

type mockclient struct{}

func (m mockclient) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
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

func TestCollector_getOrCreateCommitteeTrace(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)

	dutyStore := store.New(db)
	_, vstore, _ := storage.NewSharesStorage(networkconfig.NetworkConfig{}, db, nil)

	var committeeID = spectypes.CommitteeID{1}

	t.Run("slot > last evicted", func(t *testing.T) {
		collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
		slot := phase0.Slot(10)
		collector.lastEvictedSlot.Store(uint64(5))

		t.Run("committee not found", func(t *testing.T) {
			trace, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			require.False(t, late)
			require.NotNil(t, trace)
			require.Equal(t, slot, trace.Slot)
			require.Equal(t, committeeID, trace.CommitteeID)
		})

		t.Run("committee found, slot not found", func(t *testing.T) {
			_, _, err := collector.getOrCreateCommitteeTrace(slot-1, committeeID) // create for committee
			require.NoError(t, err)

			trace, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			require.False(t, late)
			require.NotNil(t, trace)
			require.Equal(t, slot, trace.Slot)
		})

		t.Run("committee and slot found", func(t *testing.T) {
			trace1, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)

			trace2, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			require.False(t, late)
			require.Same(t, trace1, trace2)
		})
	})

	t.Run("slot <= last evicted", func(t *testing.T) {
		slot := phase0.Slot(4)
		evictionSlot := phase0.Slot(5)

		t.Run("committeeID is in flight", func(t *testing.T) {
			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))
			_, _ = collector.inFlightCommittee.GetOrSet(committeeID, struct{}{})

			_, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.ErrorIs(t, err, errInFlight)
			require.False(t, late)
		})

		t.Run("committeeID not found on disk", func(t *testing.T) {
			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))

			trace, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			require.True(t, late)
			require.NotNil(t, trace)
			require.Equal(t, slot, trace.Slot)
			require.Equal(t, committeeID, trace.CommitteeID)
		})

		t.Run("committeeID found on disk", func(t *testing.T) {
			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			// Setup: Create a collector, save a trace, and then evict it to disk.
			trace, _, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			trace.OperatorIDs = []uint64{1, 2, 3}
			collector.store.SaveCommitteeDuties(slot, []*model.CommitteeDutyTrace{trace.trace()})
			collector.lastEvictedSlot.Store(uint64(slot))
			// Test: Create a new collector to ensure cache is empty and get the trace.
			diskTrace, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.NoError(t, err)
			require.True(t, late)
			require.NotNil(t, diskTrace)
			require.Equal(t, trace.OperatorIDs, diskTrace.OperatorIDs)
			require.Equal(t, slot, diskTrace.Slot)
			require.Equal(t, committeeID, diskTrace.CommitteeID)
		})

		t.Run("getCommitteeDutyFromDisk errors", func(t *testing.T) {
			innerErr := errors.New("error")
			dutyTraceStore := &mockDutyTraceStore{err: innerErr}

			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyTraceStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))

			trace, late, err := collector.getOrCreateCommitteeTrace(slot, committeeID)
			require.ErrorIs(t, err, innerErr)
			require.False(t, late)
			require.Nil(t, trace)
			require.False(t, collector.inFlightCommittee.Has(committeeID))
		})
	})
}

func TestCollector_getOrCreateValidatorTrace(t *testing.T) {
	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)

	dutyStore := store.New(db)
	_, vstore, _ := storage.NewSharesStorage(networkconfig.NetworkConfig{}, db, nil)

	var vPubKey = spectypes.ValidatorPK{1}
	var role = spectypes.BNRoleAggregator

	t.Run("slot > last evicted", func(t *testing.T) {
		collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
		slot := phase0.Slot(10)
		collector.lastEvictedSlot.Store(uint64(5))

		t.Run("validator not found", func(t *testing.T) {
			trace, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)
			require.False(t, late)
			require.NotNil(t, trace)
			roleTrace := trace.getOrCreate(slot, role)
			require.Equal(t, slot, roleTrace.Slot)
			require.Equal(t, role, roleTrace.Role)
		})

		t.Run("validator found, slot not found", func(t *testing.T) {
			_, _, err := collector.getOrCreateValidatorTrace(slot-1, role, vPubKey) // create for validator
			require.NoError(t, err)

			trace, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)
			require.False(t, late)
			require.NotNil(t, trace)
			roleTrace := trace.getOrCreate(slot, role)
			require.Equal(t, slot, roleTrace.Slot)
		})

		t.Run("validator and slot found", func(t *testing.T) {
			trace1, _, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)

			trace2, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)
			require.False(t, late)
			require.Same(t, trace1, trace2)
		})
	})

	t.Run("slot <= last evicted", func(t *testing.T) {
		slot := phase0.Slot(4)
		evictionSlot := phase0.Slot(5)

		t.Run("validator is in flight", func(t *testing.T) {
			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))
			_, _ = collector.inFlightValidator.GetOrSet(vPubKey, struct{}{})

			_, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.ErrorIs(t, err, errInFlight)
			require.False(t, late)
		})

		t.Run("validator not found on disk", func(t *testing.T) {
			ctrl := gomock.NewController(t)
			vstore := registrystoragemocks.NewMockValidatorStore(ctrl)

			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))

			vstore.EXPECT().ValidatorIndex(vPubKey).Return(phase0.ValidatorIndex(1), true)

			trace, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)
			require.True(t, late)
			require.NotNil(t, trace)
			roleTrace := trace.getOrCreate(slot, role)
			require.Equal(t, slot, roleTrace.Slot)
			require.Equal(t, role, roleTrace.Role)
		})

		t.Run("getValidatorDutiesFromDisk errors", func(t *testing.T) {
			innerErr := errors.New("error")
			dutyStore := &mockDutyTraceStore{err: innerErr}

			ctrl := gomock.NewController(t)
			vstore := registrystoragemocks.NewMockValidatorStore(ctrl)

			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
			collector.lastEvictedSlot.Store(uint64(evictionSlot))

			vstore.EXPECT().ValidatorIndex(vPubKey).Return(phase0.ValidatorIndex(1), true)

			trace, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.ErrorIs(t, err, innerErr)
			require.False(t, late)
			require.Nil(t, trace)
			require.False(t, collector.inFlightValidator.Has(vPubKey))
		})

		t.Run("success late message", func(t *testing.T) {
			ctrl := gomock.NewController(t)
			vstore := registrystoragemocks.NewMockValidatorStore(ctrl)

			collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

			vstore.EXPECT().ValidatorIndex(vPubKey).Return(phase0.ValidatorIndex(1), true).AnyTimes()

			_, _, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)

			collector.dumpValidatorToDBPeriodically(slot)

			collector.inFlightValidator.Delete(vPubKey)

			collector.lastEvictedSlot.Store(uint64(slot))
			trace, late, err := collector.getOrCreateValidatorTrace(slot, role, vPubKey)
			require.NoError(t, err)
			require.True(t, late)
			require.NotNil(t, trace)
			roleTrace := trace.getOrCreate(slot, role)
			require.Equal(t, slot, roleTrace.Slot)
			require.Equal(t, role, roleTrace.Role)
		})
	})

	t.Run("get or create validator duty", func(t *testing.T) {
		dt := new(validatorDutyTrace)
		m := dt.getOrCreate(1, role)
		require.Equal(t, phase0.Slot(1), m.Slot)
		require.Equal(t, role, m.Role)
	})
}

func TestValidatorDutyTrace_toBNRole(t *testing.T) {
	tests := []struct {
		role spectypes.RunnerRole
		want spectypes.BeaconRole
		err  bool
	}{
		{spectypes.RoleProposer, spectypes.BNRoleProposer, false},
		{spectypes.RoleAggregator, spectypes.BNRoleAggregator, false},
		{spectypes.RoleSyncCommitteeContribution, spectypes.BNRoleSyncCommitteeContribution, false},
		{spectypes.RoleValidatorRegistration, spectypes.BNRoleValidatorRegistration, false},
		{spectypes.RoleVoluntaryExit, spectypes.BNRoleVoluntaryExit, false},
		{spectypes.RoleCommittee, spectypes.BNRoleUnknown, true},
	}

	for _, test := range tests {
		t.Run(test.role.String(), func(t *testing.T) {
			got, err := toBNRole(test.role)
			if !test.err {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestCollector_saveLateValidatorToCommiteeLinks(t *testing.T) {
	ctrl := gomock.NewController(t)
	vstore := registrystoragemocks.NewMockValidatorStore(ctrl)
	dutyStore := new(mockDutyTraceStore)

	slot := phase0.Slot(10)

	partialSigMessage := &spectypes.PartialSignatureMessages{
		Messages: []*spectypes.PartialSignatureMessage{
			{ValidatorIndex: 1},
		},
	}
	committeeID := spectypes.CommitteeID{1}

	t.Run("save late validator to committee links", func(t *testing.T) {
		collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

		var called bool
		dutyStore.saveCommitteeDutyLinkFn = func(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error {
			called = true
			assert.Equal(t, slot, slot)
			assert.Equal(t, index, phase0.ValidatorIndex(1))
			assert.Equal(t, id, committeeID)
			return nil
		}

		collector.saveLateValidatorToCommiteeLinks(slot, partialSigMessage, committeeID)

		require.True(t, called)
	})

	t.Run("save late validator to committee links error", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)
		collector := New(t.Context(), logger, vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

		var called bool
		dutyStore.saveCommitteeDutyLinkFn = func(phase0.Slot, phase0.ValidatorIndex, spectypes.CommitteeID) error {
			called = true
			return errors.New("error")
		}

		collector.saveLateValidatorToCommiteeLinks(slot, partialSigMessage, committeeID)

		require.True(t, called)
		require.Equal(t, 1, len(logs.All()))

		entry := logs.All()[0]
		require.Equal(t, "save late validator to committee links", entry.Message)
		require.Equal(t, zap.Error(errors.New("error")), entry.Context[0])
		require.Equal(t, zap.Uint64("slot", uint64(slot)), entry.Context[1])
		require.Equal(t, zap.Uint64("validator_index", uint64(phase0.ValidatorIndex(1))), entry.Context[2])
		require.Equal(t, zap.String("committee_id", hex.EncodeToString(committeeID[:])), entry.Context[3])
	})

}

func TestCollector_lateMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	vstore := registrystoragemocks.NewMockValidatorStore(ctrl)
	dutyStore := new(mockDutyTraceStore)

	t.Run("late message in flight", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)
		collector := New(t.Context(), logger, vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

		msgID := spectypes.NewMsgID(spectypes.DomainType{1}, []byte{1}, spectypes.RoleCommittee)

		msg := &queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.MessageID{1},
			},
			Body: &specqbft.Message{
				Height:     1,
				MsgType:    specqbft.RoundChangeMsgType,
				Identifier: msgID[:],
			},
		}

		var committeeID spectypes.CommitteeID
		copy(committeeID[:], msgID.GetDutyExecutorID()[16:])
		collector.inFlightCommittee.Set(committeeID, struct{}{})
		collector.lastEvictedSlot.Store(uint64(1))

		go func() {
			time.Sleep(time.Millisecond * 100)
			collector.inFlightCommittee.Delete(committeeID)
		}()

		dutyStore.err = errors.New("error")
		collector.collectLateMessage(t.Context(), msg, nil)

		require.Equal(t, 1, len(logs.All()))
		entry := logs.All()[0]
		require.Equal(t, "collect late message", entry.Message)
		err := entry.Context[0].Interface.(error)
		require.ErrorIs(t, err, dutyStore.err)
	})

	t.Run("late message in flight exhausted retries", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)
		collector := New(t.Context(), logger, vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

		msgID := spectypes.NewMsgID(spectypes.DomainType{1}, []byte{1}, spectypes.RoleCommittee)

		msg := &queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.MessageID{1},
			},
			Body: &specqbft.Message{
				Height:     1,
				MsgType:    specqbft.RoundChangeMsgType,
				Identifier: msgID[:],
			},
		}

		var committeeID spectypes.CommitteeID
		copy(committeeID[:], msgID.GetDutyExecutorID()[16:])
		collector.inFlightCommittee.Set(committeeID, struct{}{})
		collector.lastEvictedSlot.Store(uint64(1))
		collector.collectLateMessage(t.Context(), msg, nil)

		require.Equal(t, 2, len(logs.All()))

		entry1 := logs.All()[0]
		require.Equal(t, "exhausted retries for late message", entry1.Message)
		require.Equal(t, zap.Int("tries", 3), entry1.Context[1])

		entry2 := logs.All()[1]
		require.Equal(t, "collect late message", entry2.Message)
		require.Equal(t, zap.Error(errors.New("in flight")), entry2.Context[0])
	})

	t.Run("success", func(t *testing.T) {
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(core)
		collector := New(t.Context(), logger, vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

		msg := &queue.SSVMessage{
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.MessageID{1},
			},
		}

		collector.collectLateMessage(t.Context(), msg, nil)

		require.Equal(t, 0, len(logs.All()))
	})
}

func TestEvict(t *testing.T) {
	t.Run("evict updates last evicted slot", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		vstore := registrystoragemocks.NewMockValidatorStore(ctrl)
		dutyStore := new(mockDutyTraceStore)

		slot := phase0.Slot(10)

		collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)
		collector.evict(slot)

		threshold := slot - slotTTL
		require.Equal(t, uint64(threshold), collector.lastEvictedSlot.Load())
	})
}

func TestCollector_GetCommitteeID(t *testing.T) {
	slot := phase0.Slot(10)
	var vPubKey = spectypes.ValidatorPK{1}

	ctrl := gomock.NewController(t)
	vstore := registrystoragemocks.NewMockValidatorStore(ctrl)
	dutyStore := new(mockDutyTraceStore)

	collector := New(t.Context(), zap.NewNop(), vstore, nil, dutyStore, networkconfig.TestNetwork.BeaconConfig)

	slotToCommittee := hashmap.New[phase0.Slot, spectypes.CommitteeID]()
	slotToCommittee.Set(slot, spectypes.CommitteeID{1})
	collector.validatorIndexToCommitteeLinks.Set(phase0.ValidatorIndex(1), slotToCommittee)

	vstore.EXPECT().ValidatorIndex(vPubKey).Return(phase0.ValidatorIndex(1), true)

	committeeID, index, err := collector.GetCommitteeID(slot, vPubKey)
	require.NoError(t, err)
	require.Equal(t, committeeID, spectypes.CommitteeID{1})
	require.Equal(t, index, phase0.ValidatorIndex(1))
}

type mockDutyTraceStore struct {
	err                     error
	committeeDutyTrace      *model.CommitteeDutyTrace
	saveCommitteeDutyLinkFn func(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error
}

func (m *mockDutyTraceStore) SaveCommitteeDuties(slot phase0.Slot, duties []*model.CommitteeDutyTrace) error {
	return m.err
}

func (m *mockDutyTraceStore) SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error {
	if m.saveCommitteeDutyLinkFn != nil {
		return m.saveCommitteeDutyLinkFn(slot, index, id)
	}
	return m.err
}

func (m *mockDutyTraceStore) SaveCommitteeDutyLinks(slot phase0.Slot, linkMap map[phase0.ValidatorIndex]spectypes.CommitteeID) error {
	return m.err
}

func (m *mockDutyTraceStore) SaveCommitteeDuty(duty *model.CommitteeDutyTrace) error {
	return m.err
}

func (m *mockDutyTraceStore) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	return m.committeeDutyTrace, m.err
}

func (m *mockDutyTraceStore) GetCommitteeDuties(slot phase0.Slot) ([]*model.CommitteeDutyTrace, error) {
	return nil, m.err
}

func (m *mockDutyTraceStore) SaveValidatorDuties(duties []*model.ValidatorDutyTrace) error {
	return m.err
}

func (m *mockDutyTraceStore) SaveValidatorDuty(duty *model.ValidatorDutyTrace) error {
	return m.err
}

func (m *mockDutyTraceStore) GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*model.ValidatorDutyTrace, error) {
	return nil, m.err
}

func (m *mockDutyTraceStore) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*model.ValidatorDutyTrace, error) {
	return nil, m.err
}

func (m *mockDutyTraceStore) GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	return spectypes.CommitteeID{}, m.err
}

func (m *mockDutyTraceStore) GetCommitteeDutyLinks(slot phase0.Slot) ([]*model.CommitteeDutyLink, error) {
	return nil, m.err
}
