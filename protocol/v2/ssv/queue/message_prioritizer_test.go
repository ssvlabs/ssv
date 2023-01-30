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
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
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
			Height:             100,
			Slot:               64,
			Quorum:             4,
		},
		messages: []mockMessage{
			// 1. Current height/slot:
			// 1.1. Consensus
			// 1.1.1. Consensus/Proposal
			mockConsensusMessage{Height: 100, Type: qbft.ProposalMsgType},
			// 1.1.2. Consensus/Prepare
			mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType},
			// 1.1.3. Consensus/Commit
			mockConsensusMessage{Height: 100, Type: qbft.CommitMsgType},
			// 1.1.4. Consensus/<Other>
			mockConsensusMessage{Height: 100, Type: qbft.RoundChangeMsgType},
			// 1.2. Pre-consensus
			// mockNonConsensusMessage{Slot: 64, Type: ssv.SelectionProofPartialSig},
			// 1.3. Post-consensus
			// mockNonConsensusMessage{Slot: 64, Type: ssv.PostConsensusPartialSig},

			// 2. Higher height/slot:
			// 2.1 Decided
			mockConsensusMessage{Height: 101, Decided: true},
			// 2.2. Pre-consensus
			// mockNonConsensusMessage{Slot: 65, Type: ssv.SelectionProofPartialSig},
			// 2.3. Consensus
			mockConsensusMessage{Height: 101},
			// 2.4. Post-consensus
			// mockNonConsensusMessage{Slot: 65, Type: ssv.PostConsensusPartialSig},

			// 3. Lower height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 3.2. Commit
			mockConsensusMessage{Height: 99, Type: qbft.CommitMsgType},
			// 3.3. Pre-consensus
			// mockNonConsensusMessage{Slot: 63, Type: ssv.SelectionProofPartialSig},
		},
	},
	{
		name: "No running instance",
		state: &State{
			HasRunningInstance: false,
			Height:             100,
			Slot:               64,
			Quorum:             4,
		},
		messages: []mockMessage{
			// 1. Current height/slot:
			// 1.1. Pre-consensus
			// mockNonConsensusMessage{Slot: 64, Type: ssv.SelectionProofPartialSig},
			// 1.2. Post-consensus
			// mockNonConsensusMessage{Slot: 64, Type: ssv.PostConsensusPartialSig},
			// 1.3. Consensus
			// 1.3.1. Consensus/Proposal
			mockConsensusMessage{Height: 100, Type: qbft.ProposalMsgType},
			// 1.3.2. Consensus/Prepare
			mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType},
			// 1.3.3. Consensus/Commit
			mockConsensusMessage{Height: 100, Type: qbft.CommitMsgType},
			// 1.3.4. Consensus/<Other>
			mockConsensusMessage{Height: 100, Type: qbft.RoundChangeMsgType},

			// 2. Higher height/slot:
			// 2.1 Decided
			mockConsensusMessage{Height: 101, Decided: true},
			// 2.2. Pre-consensus
			// mockNonConsensusMessage{Slot: 65, Type: ssv.SelectionProofPartialSig},
			// 2.3. Consensus
			mockConsensusMessage{Height: 101},
			// 2.4. Post-consensus
			// mockNonConsensusMessage{Slot: 65, Type: ssv.PostConsensusPartialSig},

			// 3. Lower height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 3.2. Commit
			mockConsensusMessage{Height: 99, Type: qbft.CommitMsgType},
			// 3.3. Pre-consensus
			// mockNonConsensusMessage{Slot: 63, Type: ssv.SelectionProofPartialSig},
		},
	},
}

func TestMessagePrioritizer(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(test.name, func(t *testing.T) {
			messages := make(messageSlice, len(test.messages))
			for i, m := range test.messages {
				var err error
				messages[i], err = DecodeSSVMessage(m.ssvMessage(test.state))
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

type mockMessage interface {
	ssvMessage(*State) *types.SSVMessage
}

type mockConsensusMessage struct {
	Role    types.BeaconRole
	Type    qbft.MessageType
	Decided bool
	Height  qbft.Height
}

func (m mockConsensusMessage) ssvMessage(state *State) *types.SSVMessage {
	var (
		typ         = m.Type
		signerCount = 1
	)
	if m.Decided {
		typ = qbft.CommitMsgType
		signerCount = int(state.Quorum) + 1
	}

	var signers []types.OperatorID
	for i := 0; i < signerCount; i++ {
		signers = append(signers, types.OperatorID(i))
	}

	factory := ssvMessageFactory(m.Role)
	return factory(
		&qbft.SignedMessage{
			Message: &qbft.Message{
				MsgType:    typ,
				Height:     m.Height,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       []byte{1, 2, 3, 4},
			},
			Signature: []byte{1, 2, 3, 4},
			Signers:   signers,
		},
		nil,
	)
}

//type mockNonConsensusMessage struct {
//	Role types.BeaconRole
//	Type ssv.PartialSigMsgType
//	Slot phase0.Slot
//}
//
//func (m mockNonConsensusMessage) ssvMessage(state *State) *types.SSVMessage {
//	factory := ssvMessageFactory(m.Role)
//	return factory(
//		nil,
//		&ssv.SignedPartialSignatureMessage{
//			Message: ssv.PartialSignatureMessages{
//				Type: m.Type,
//				Messages: []*ssv.PartialSignatureMessage{
//					{
//						// Slot:             m.Slot,
//						PartialSignature: []byte{},
//						SigningRoot:      []byte{},
//						Signer:           0,
//					},
//				},
//			},
//			Signature: []byte{1, 2, 3, 4},
//			Signer:    types.OperatorID(1),
//		},
//	)
//}

type messageSlice []*DecodedSSVMessage

func (m messageSlice) shuffle() messageSlice {
	shuffled := make([]*DecodedSSVMessage, len(m))
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
			typ          interface{}
			heightOrSlot interface{}
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
		case *ssv.SignedPartialSignatureMessage:
			// heightOrSlot = mm.Message.Messages[0].Slot
			typ = mm.Message.Type
			if typ == ssv.PostConsensusPartialSig {
				kind = "post-consensus"
			} else {
				kind = "pre-consensus"
			}
		case *qbft.SignedMessage:
			kind = "consensus"
			heightOrSlot = mm.Message.Height
			typ = mm.Message.MsgType
		}

		tbl.AddRow(
			fmt.Sprint(i),
			kind,
			fmt.Sprintf("%d (%s)", heightOrSlot, relation),
			fmt.Sprint(typ),
			fmt.Sprintf("%t", isDecidedMesssage(s, msg)),
		)
	}
	tbl.Render()
	return b.String()
}

func ssvMessageFactory(role types.BeaconRole) func(*qbft.SignedMessage, *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	switch role {
	case types.BNRoleAttester:
		return testingutils.SSVMsgAttester
	case types.BNRoleProposer:
		return testingutils.SSVMsgProposer
	case types.BNRoleAggregator:
		return testingutils.SSVMsgAggregator
	case types.BNRoleSyncCommittee:
		return testingutils.SSVMsgSyncCommittee
	case types.BNRoleSyncCommitteeContribution:
		return testingutils.SSVMsgSyncCommitteeContribution
	default:
		panic("invalid role")
	}
}
