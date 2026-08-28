package queue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/aquasecurity/table"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/utils/casts"
)

var messagePriorityTests = []struct {
	name     string
	state    *State
	messages []mockMessage
}{
	{
		name: "Running instance",
		state: &State{
			HasRunningInstance: true,
			Slot:               100,
			Quorum:             4,
		},
		messages: []mockMessage{
			// 1. Events:
			// 1.1. Events/ExecuteDuty
			mockExecuteDutyMessage{Slot: 62, Role: spectypes.BNRoleProposer},
			// 1.2. Events/Timeout
			mockTimeoutMessage{Slot: 98, Role: spectypes.RoleProposer},

			// 2. Current height/slot:
			// 2.1. Consensus
			// 2.1.1. Consensus/Proposal
			mockConsensusMessage{Height: 100, Type: specqbft.ProposalMsgType},
			// 2.1.2. Consensus/Prepare
			mockConsensusMessage{Height: 100, Type: specqbft.PrepareMsgType},
			// 2.1.3. Consensus/Commit
			mockConsensusMessage{Height: 100, Type: specqbft.CommitMsgType},
			// 2.1.4. Consensus/<Other>
			mockConsensusMessage{Height: 100, Type: specqbft.RoundChangeMsgType},
			// 2.2. Pre-consensus
			mockNonConsensusMessage{Slot: 100, Type: types.SelectionProofPartialSig},
			// 2.3. Post-consensus
			mockNonConsensusMessage{Slot: 100, Type: spectypes.PostConsensusPartialSig},

			// 3. Higher height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 101, Decided: true},
			// 3.2. Pre-consensus
			mockNonConsensusMessage{Slot: 101, Type: types.SelectionProofPartialSig},
			// 3.3. Consensus
			mockConsensusMessage{Height: 101},
			// 3.4. Post-consensus
			mockNonConsensusMessage{Slot: 101, Type: spectypes.PostConsensusPartialSig},

			// 4. Lower height/slot:
			// 4.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 4.2. Commit
			mockConsensusMessage{Height: 99, Type: specqbft.CommitMsgType},
			// 4.3. Pre-consensus
			mockNonConsensusMessage{Slot: 99, Type: types.SelectionProofPartialSig},
		},
	},
	{
		name: "No running instance",
		state: &State{
			HasRunningInstance: false,
			Slot:               100,
			Quorum:             4,
		},
		messages: []mockMessage{
			// 1. Current height/slot:
			// 1.1. Pre-consensus
			mockNonConsensusMessage{Slot: 100, Type: types.SelectionProofPartialSig},
			// 1.2. Post-consensus
			mockNonConsensusMessage{Slot: 100, Type: spectypes.PostConsensusPartialSig},
			// 1.3. Consensus
			// 1.3.1. Consensus/Proposal
			mockConsensusMessage{Height: 100, Type: specqbft.ProposalMsgType},
			// 1.3.2. Consensus/Prepare
			mockConsensusMessage{Height: 100, Type: specqbft.PrepareMsgType},
			// 1.3.3. Consensus/Commit
			mockConsensusMessage{Height: 100, Type: specqbft.CommitMsgType},
			// 1.3.4. Consensus/<Other>
			mockConsensusMessage{Height: 100, Type: specqbft.RoundChangeMsgType},

			// 2. Higher height/slot:
			// 2.1 Decided
			mockConsensusMessage{Height: 101, Decided: true},
			// 2.2. Pre-consensus
			mockNonConsensusMessage{Slot: 101, Type: types.SelectionProofPartialSig},
			// 2.3. Consensus
			mockConsensusMessage{Height: 101},
			// 2.4. Post-consensus
			mockNonConsensusMessage{Slot: 101, Type: spectypes.PostConsensusPartialSig},

			// 3. Lower height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 3.2. Commit
			mockConsensusMessage{Height: 99, Type: specqbft.CommitMsgType},
			// 3.3. Pre-consensus
			mockNonConsensusMessage{Slot: 99, Type: types.SelectionProofPartialSig},
		},
	},
}

func TestMessagePrioritizer(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(test.name, func(t *testing.T) {
			messages := make(messageSlice, len(test.messages))
			for i, m := range test.messages {
				var err error
				messages[i], err = DecodeSignedSSVMessage(m.ssvMessage(test.state))
				require.NoError(t, err)
			}

			var shuffles []messageSlice
			for {
				shuffledMessages := messages.shuffle()
				if shuffledMessages.equal(messages) {
					continue
				}
				shuffles = append(shuffles, shuffledMessages)
				if len(shuffles) == 10 {
					break
				}
			}

			prioritizer := NewMessagePrioritizer(test.state)
			for _, shuffle := range shuffles {
				shuffle.sort(prioritizer)
				correctOrder := messages.equal(shuffle)
				if !correctOrder {
					require.Fail(t, "incorrect order:\n"+shuffle.dump(test.state))
				}
			}
		})
	}
}

func TestMessagePrioritizer_LowerHeightCommitOutranksLowerSlotPreConsensusAtAdvancedRound(t *testing.T) {
	state := &State{
		HasRunningInstance: true,
		Slot:               100,
		Round:              3,
		Quorum:             4,
	}

	lowerHeightCommit, err := DecodeSignedSSVMessage(
		mockConsensusMessage{Height: 99, Type: specqbft.CommitMsgType}.ssvMessage(state),
	)
	require.NoError(t, err)

	lowerSlotPreConsensus, err := DecodeSignedSSVMessage(
		mockNonConsensusMessage{Slot: 99, Type: types.SelectionProofPartialSig}.ssvMessage(state),
	)
	require.NoError(t, err)

	prioritizer := NewMessagePrioritizer(state)
	require.True(t, prioritizer.Prior(lowerHeightCommit, lowerSlotPreConsensus))
	require.False(t, prioritizer.Prior(lowerSlotPreConsensus, lowerHeightCommit))
}

// TestMessagePrioritizer_LowerHeightCommitOutranksOtherLowerHeightConsensus verifies that at lower
// QBFT heights, a Commit message outranks any other consensus message type. This guards against the
// historical type-confusion in scoreMessageSubtype where the lower-height Commit branch never matched
// (it cast SSVMessage.MsgType to specqbft.MessageType instead of reading the inner QBFT MsgType),
// which let scoreConsensusType promote stale Proposal/Prepare messages above stale Commits.
func TestMessagePrioritizer_LowerHeightCommitOutranksOtherLowerHeightConsensus(t *testing.T) {
	state := &State{
		HasRunningInstance: true,
		Slot:               100,
		Quorum:             4,
	}

	cases := []struct {
		name      string
		otherType specqbft.MessageType
	}{
		{name: "Proposal", otherType: specqbft.ProposalMsgType},
		{name: "Prepare", otherType: specqbft.PrepareMsgType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lowerHeightCommit, err := DecodeSignedSSVMessage(
				mockConsensusMessage{Height: 99, Type: specqbft.CommitMsgType}.ssvMessage(state),
			)
			require.NoError(t, err)

			lowerHeightOther, err := DecodeSignedSSVMessage(
				mockConsensusMessage{Height: 99, Type: tc.otherType}.ssvMessage(state),
			)
			require.NoError(t, err)

			prioritizer := NewMessagePrioritizer(state)
			require.True(t, prioritizer.Prior(lowerHeightCommit, lowerHeightOther),
				"expected lower-height Commit to outrank lower-height %s", tc.name)
			require.False(t, prioritizer.Prior(lowerHeightOther, lowerHeightCommit),
				"expected lower-height %s to NOT outrank lower-height Commit", tc.name)
		})
	}
}

// TestCommitteeQueuePrioritizer_LowerHeightCommitOutranksOtherLowerHeightConsensus mirrors the
// validator-path test above for the committee prioritizer. The committee chain has no scoreRound
// step, but the same scoreCommitteeMessageSubtype type-confusion existed in its lower-height
// branch — letting scoreConsensusType promote stale Proposals/Prepares above stale Commits.
func TestCommitteeQueuePrioritizer_LowerHeightCommitOutranksOtherLowerHeightConsensus(t *testing.T) {
	state := &State{
		HasRunningInstance: true,
		Slot:               100,
		Quorum:             4,
	}

	cases := []struct {
		name      string
		otherType specqbft.MessageType
	}{
		{name: "Proposal", otherType: specqbft.ProposalMsgType},
		{name: "Prepare", otherType: specqbft.PrepareMsgType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lowerHeightCommit, err := DecodeSignedSSVMessage(
				mockConsensusMessage{Height: 99, Type: specqbft.CommitMsgType}.ssvMessage(state),
			)
			require.NoError(t, err)

			lowerHeightOther, err := DecodeSignedSSVMessage(
				mockConsensusMessage{Height: 99, Type: tc.otherType}.ssvMessage(state),
			)
			require.NoError(t, err)

			prioritizer := NewCommitteeQueuePrioritizer(state)
			require.True(t, prioritizer.Prior(lowerHeightCommit, lowerHeightOther),
				"expected lower-height Commit to outrank lower-height %s (committee)", tc.name)
			require.False(t, prioritizer.Prior(lowerHeightOther, lowerHeightCommit),
				"expected lower-height %s to NOT outrank lower-height Commit (committee)", tc.name)
		})
	}
}

type mockMessage interface {
	ssvMessage(*State) *spectypes.SignedSSVMessage
}

type mockConsensusMessage struct {
	Role    spectypes.RunnerRole
	Type    specqbft.MessageType
	Decided bool
	Height  specqbft.Height
}

func (m mockConsensusMessage) ssvMessage(state *State) *spectypes.SignedSSVMessage {
	var (
		typ         = m.Type
		signerCount = 1
	)
	if m.Decided {
		typ = specqbft.CommitMsgType
		signerCount = int(state.Quorum) + 1
	}

	signers := make([]spectypes.OperatorID, 0, signerCount)
	for i := 0; i < signerCount; i++ {
		signers = append(signers, spectypes.OperatorID(i))
	}

	factory := ssvMessageFactory(m.Role)
	msg := specqbft.Message{
		MsgType:                  typ,
		Height:                   m.Height,
		Round:                    2,
		Identifier:               make([]byte, 56),
		Root:                     [32]byte{1, 2, 3},
		RoundChangeJustification: [][]byte{{1, 2, 3, 4}},
		PrepareJustification:     [][]byte{{1, 2, 3, 4}},
	}
	copy(msg.Identifier[:3], []byte{1, 2, 3, 4})
	msgEncoded, err := msg.Encode()
	if err != nil {
		panic(err)
	}
	signedMsg := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID(msg.Identifier),
			Data:    msgEncoded,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  make([][]byte, len(signers)),
		OperatorIDs: signers,
	}
	return &spectypes.SignedSSVMessage{
		SSVMessage:  factory(signedMsg, nil),
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  make([][]byte, len(signers)),
		OperatorIDs: signers,
	}
}

type mockNonConsensusMessage struct {
	Role spectypes.RunnerRole
	Type spectypes.PartialSigMsgType
	Slot phase0.Slot
}

func (m mockNonConsensusMessage) ssvMessage(state *State) *spectypes.SignedSSVMessage {
	factory := ssvMessageFactory(m.Role)
	partMsg := &spectypes.PartialSignatureMessages{
		Type: m.Type,
		Slot: m.Slot,
		Messages: []*spectypes.PartialSignatureMessage{{
			PartialSignature: make([]byte, 96),
			SigningRoot:      [32]byte{},
			Signer:           spectypes.OperatorID(1),
			ValidatorIndex:   phase0.ValidatorIndex(1),
		}},
	}
	msgEncoded, err := partMsg.Encode()
	if err != nil {
		panic(err)
	}
	signedMsg := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.MessageID(make([]byte, 56)),
			Data:    msgEncoded,
		},
	}
	return &spectypes.SignedSSVMessage{
		SSVMessage:  factory(signedMsg, nil),
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  make([][]byte, 1),
		OperatorIDs: []spectypes.OperatorID{1},
	}
}

type mockExecuteDutyMessage struct {
	Role spectypes.BeaconRole
	Slot phase0.Slot
}

func (m mockExecuteDutyMessage) ssvMessage(state *State) *spectypes.SignedSSVMessage {
	edd, err := json.Marshal(types.ExecuteDutyData{Duty: &spectypes.ValidatorDuty{
		Type: m.Role,
		Slot: m.Slot,
	}})
	if err != nil {
		panic(err)
	}
	data, err := (&types.EventMsg{
		Type: types.ExecuteDuty,
		Data: edd,
	}).Encode()
	if err != nil {
		panic(err)
	}
	return &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: message.SSVEventMsgType,
			MsgID:   ssvtestingutils.NewMsgID(testingutils.TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], casts.BeaconRoleToRunnerRole(m.Role)),
			Data:    data,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  make([][]byte, 1),
		OperatorIDs: []spectypes.OperatorID{1},
	}
}

type mockTimeoutMessage struct {
	Role spectypes.RunnerRole
	Slot phase0.Slot
}

func (m mockTimeoutMessage) ssvMessage(state *State) *spectypes.SignedSSVMessage {
	td := types.TimeoutData{Slot: m.Slot}
	data, err := json.Marshal(td)
	if err != nil {
		panic(err)
	}
	eventMsgData, err := (&types.EventMsg{
		Type: types.Timeout,
		Data: data,
	}).Encode()
	if err != nil {
		panic(err)
	}
	return &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{
			MsgType: message.SSVEventMsgType,
			MsgID:   ssvtestingutils.NewMsgID(testingutils.TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], m.Role),
			Data:    eventMsgData,
		},
		FullData:    []byte{1, 2, 3, 4},
		Signatures:  make([][]byte, 1),
		OperatorIDs: []spectypes.OperatorID{1},
	}
}

type messageSlice []*SSVMessage

func (m messageSlice) shuffle() messageSlice {
	shuffled := make([]*SSVMessage, len(m))
	for i, j := range rand.Perm(len(m)) {
		shuffled[i] = m[j]
	}
	return shuffled
}

func (m messageSlice) sort(prioritizer MessagePrioritizer) {
	sort.Slice(m, func(i, j int) bool {
		return prioritizer.Prior(m[i], m[j])
	})
}

func (m messageSlice) equal(m2 messageSlice) bool {
	if len(m) != len(m2) {
		return false
	}
	for i := range m {
		a, err := json.Marshal(m[i])
		if err != nil {
			panic(err)
		}
		b, err := json.Marshal(m2[i])
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}

func (m messageSlice) dump(s *State) string {
	b := &strings.Builder{}
	tbl := table.New(b)
	tbl.SetHeaders("#", "Kind", "Height/Slot", "Type", "Decided")
	for i, msg := range m {
		var (
			kind         string
			typ          any
			heightOrSlot any
			relation     string
		)

		switch compareHeightOrSlot(s, msg) {
		case -1:
			relation = "lower"
		case 0:
			relation = "current"
		case 1:
			relation = "higher"
		}

		switch mm := msg.Body.(type) {
		case *spectypes.PartialSignatureMessages:
			// heightOrSlot = mm.Message.Messages[0].Slot
			typ = mm.Type
			if typ == spectypes.PostConsensusPartialSig {
				kind = "post-consensus"
			} else {
				kind = "pre-consensus"
			}
		case *specqbft.Message:
			kind = "consensus"
			heightOrSlot = mm.Height
			typ = mm.MsgType
		}

		decided := false
		if _, ok := msg.Body.(*specqbft.Message); ok {
			decided = isDecidedMessage(s, msg)
		}
		tbl.AddRow(
			fmt.Sprint(i),
			kind,
			fmt.Sprintf("%d (%s)", heightOrSlot, relation),
			fmt.Sprint(typ),
			fmt.Sprintf("%t", decided),
		)
	}
	tbl.Render()
	return b.String()
}

func ssvMessageFactory(role spectypes.RunnerRole) func(*spectypes.SignedSSVMessage, *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	switch role {
	case spectypes.RoleCommittee:
		return testingutils.SSVMsgAttester
	case spectypes.RoleProposer:
		return testingutils.SSVMsgProposer
	case types.RoleAggregator:
		return testingutils.SSVMsgAggregator
	case types.RoleSyncCommitteeContribution:
		return testingutils.SSVMsgSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		return testingutils.SSVMsgValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		return testingutils.SSVMsgVoluntaryExit
	default:
		panic("invalid role")
	}
}
