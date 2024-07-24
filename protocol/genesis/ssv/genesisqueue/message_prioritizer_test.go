package genesisqueue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	preforkphase0 "github.com/AKorpusenko/genesis-go-eth2-client/spec/phase0"
	"github.com/aquasecurity/table"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	"github.com/ssvlabs/ssv/protocol/genesis/message"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
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
			// 1. Events:
			// 1.1. Events/ExecuteDuty
			mockExecuteDutyMessage{Slot: 62, Role: genesisspectypes.BNRoleProposer},
			// 1.2. Events/Timeout
			mockTimeoutMessage{Height: 98, Role: genesisspectypes.BNRoleProposer},

			// 2. Current height/slot:
			// 2.1. Consensus
			// 2.1.1. Consensus/Proposal
			mockConsensusMessage{Height: 100, Type: qbft.ProposalMsgType},
			// 2.1.2. Consensus/Prepare
			mockConsensusMessage{Height: 100, Type: qbft.PrepareMsgType},
			// 2.1.3. Consensus/Commit
			mockConsensusMessage{Height: 100, Type: qbft.CommitMsgType},
			// 2.1.4. Consensus/<Other>
			mockConsensusMessage{Height: 100, Type: qbft.RoundChangeMsgType},
			// 2.2. Pre-consensus
			mockNonConsensusMessage{Slot: 64, Type: genesisspectypes.SelectionProofPartialSig},
			// 2.3. Post-consensus
			mockNonConsensusMessage{Slot: 64, Type: genesisspectypes.PostConsensusPartialSig},

			// 3. Higher height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 101, Decided: true},
			// 3.2. Pre-consensus
			mockNonConsensusMessage{Slot: 65, Type: genesisspectypes.SelectionProofPartialSig},
			// 3.3. Consensus
			mockConsensusMessage{Height: 101},
			// 3.4. Post-consensus
			mockNonConsensusMessage{Slot: 65, Type: genesisspectypes.PostConsensusPartialSig},

			// 4. Lower height/slot:
			// 4.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 4.2. Commit
			mockConsensusMessage{Height: 99, Type: qbft.CommitMsgType},
			// 4.3. Pre-consensus
			mockNonConsensusMessage{Slot: 63, Type: genesisspectypes.SelectionProofPartialSig},
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
			mockNonConsensusMessage{Slot: 64, Type: genesisspectypes.SelectionProofPartialSig},
			// 1.2. Post-consensus
			mockNonConsensusMessage{Slot: 64, Type: genesisspectypes.PostConsensusPartialSig},
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
			mockNonConsensusMessage{Slot: 65, Type: genesisspectypes.SelectionProofPartialSig},
			// 2.3. Consensus
			mockConsensusMessage{Height: 101},
			// 2.4. Post-consensus
			mockNonConsensusMessage{Slot: 65, Type: genesisspectypes.PostConsensusPartialSig},

			// 3. Lower height/slot:
			// 3.1 Decided
			mockConsensusMessage{Height: 99, Decided: true},
			// 3.2. Commit
			mockConsensusMessage{Height: 99, Type: qbft.CommitMsgType},
			// 3.3. Pre-consensus
			mockNonConsensusMessage{Slot: 63, Type: genesisspectypes.SelectionProofPartialSig},
		},
	},
}

func TestMessagePrioritizer(t *testing.T) {
	for _, test := range messagePriorityTests {
		t.Run(test.name, func(t *testing.T) {
			messages := make(messageSlice, len(test.messages))
			for i, m := range test.messages {
				var err error
				messages[i], err = DecodeGenesisSSVMessage(m.ssvMessage(test.state))
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
	ssvMessage(*State) *genesisspectypes.SSVMessage
}

type mockConsensusMessage struct {
	Role    genesisspectypes.BeaconRole
	Type    qbft.MessageType
	Decided bool
	Height  qbft.Height
}

func (m mockConsensusMessage) ssvMessage(state *State) *genesisspectypes.SSVMessage {
	var (
		typ         = m.Type
		signerCount = 1
	)
	if m.Decided {
		typ = qbft.CommitMsgType
		signerCount = int(state.Quorum) + 1
	}

	var signers []genesisspectypes.OperatorID
	for i := 0; i < signerCount; i++ {
		signers = append(signers, genesisspectypes.OperatorID(i))
	}

	factory := ssvMessageFactory(m.Role)
	return factory(
		&qbft.SignedMessage{
			Message: qbft.Message{
				MsgType:                  typ,
				Height:                   m.Height,
				Round:                    2,
				Identifier:               []byte{1, 2, 3, 4},
				Root:                     [32]byte{1, 2, 3},
				RoundChangeJustification: [][]byte{{1, 2, 3, 4}},
				PrepareJustification:     [][]byte{{1, 2, 3, 4}},
			},

			FullData:  []byte{1, 2, 3, 4},
			Signature: make([]byte, 96),
			Signers:   signers,
		},
		nil,
	)
}

type mockNonConsensusMessage struct {
	Role genesisspectypes.BeaconRole
	Type genesisspectypes.PartialSigMsgType
	Slot phase0.Slot
}

func (m mockNonConsensusMessage) ssvMessage(state *State) *genesisspectypes.SSVMessage {
	factory := ssvMessageFactory(m.Role)
	return factory(
		nil,
		&genesisspectypes.SignedPartialSignatureMessage{
			Message: genesisspectypes.PartialSignatureMessages{
				Type: m.Type,
				Slot: preforkphase0.Slot(m.Slot),
				Messages: []*genesisspectypes.PartialSignatureMessage{
					{
						PartialSignature: make([]byte, 96),
						SigningRoot:      [32]byte{},
						Signer:           1,
					},
				},
			},
			Signature: make([]byte, 96),
			Signer:    genesisspectypes.OperatorID(1),
		},
	)
}

type mockExecuteDutyMessage struct {
	Role genesisspectypes.BeaconRole
	Slot phase0.Slot
}

func (m mockExecuteDutyMessage) ssvMessage(state *State) *genesisspectypes.SSVMessage {
	edd, err := json.Marshal(types.ExecuteDutyData{Duty: &genesisspectypes.Duty{
		Type: m.Role,
		Slot: preforkphase0.Slot(m.Slot),
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
	return &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.MsgType(message.SSVEventMsgType),
		MsgID:   genesisspectypes.NewMsgID(testingutils.TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], m.Role),
		Data:    data,
	}
}

type mockTimeoutMessage struct {
	Role   genesisspectypes.BeaconRole
	Height qbft.Height
}

func (m mockTimeoutMessage) ssvMessage(state *State) *genesisspectypes.SSVMessage {
	td := types.TimeoutData{Height: m.Height}
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
	return &genesisspectypes.SSVMessage{
		MsgType: genesisspectypes.MsgType(message.SSVEventMsgType),
		MsgID:   genesisspectypes.NewMsgID(testingutils.TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], m.Role),
		Data:    eventMsgData,
	}
}

type messageSlice []*GenesisSSVMessage

func (m messageSlice) shuffle() messageSlice {
	shuffled := make([]*GenesisSSVMessage, len(m))
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
		case *genesisspectypes.SignedPartialSignatureMessage:
			// heightOrSlot = mm.Message.Messages[0].Slot
			typ = mm.Message.Type
			if typ == genesisspectypes.PostConsensusPartialSig {
				kind = "post-consensus"
			} else {
				kind = "pre-consensus"
			}
		case *qbft.SignedMessage:
			kind = "consensus"
			heightOrSlot = mm.Message.Height
			typ = mm.Message.MsgType
		}

		decided := false
		if sm, ok := msg.Body.(*GenesisSSVMessage); ok {
			decided = isDecidedMessage(s, sm)
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

func ssvMessageFactory(role genesisspectypes.BeaconRole) func(*qbft.SignedMessage, *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	switch role {
	case genesisspectypes.BNRoleAttester:
		return testingutils.SSVMsgAttester
	case genesisspectypes.BNRoleProposer:
		return testingutils.SSVMsgProposer
	case genesisspectypes.BNRoleAggregator:
		return testingutils.SSVMsgAggregator
	case genesisspectypes.BNRoleSyncCommittee:
		return testingutils.SSVMsgSyncCommittee
	case genesisspectypes.BNRoleSyncCommitteeContribution:
		return testingutils.SSVMsgSyncCommitteeContribution
	case genesisspectypes.BNRoleValidatorRegistration:
		return testingutils.SSVMsgValidatorRegistration
	case genesisspectypes.BNRoleVoluntaryExit:
		return testingutils.SSVMsgVoluntaryExit
	default:
		panic("invalid role")
	}
}
