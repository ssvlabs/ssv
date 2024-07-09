package qbft

import (
	"crypto/rsa"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
)

var TestingMessage = &specqbft.Message{
	MsgType:    specqbft.ProposalMsgType,
	Height:     specqbft.FirstHeight,
	Round:      specqbft.FirstRound,
	Identifier: []byte{1, 2, 3, 4},
	Root:       [32]byte{1, 2, 3, 4},
}

var TestingSignedMsg = func() *spectypes.SignedSSVMessage {
	return testingutils.SignQBFTMsg(TestingSK, 1, TestingMessage)
}()

var TestingSK = func() *rsa.PrivateKey {
	skPem, _, _ := spectypes.GenerateKey()
	ret, _ := spectypes.PemToPrivateKey(skPem)
	return ret
}()

var TestingInstanceStruct = &specqbft.Instance{
	State: &specqbft.State{
		CommitteeMember:                 testingutils.TestingCommitteeMember(testingutils.Testing4SharesSet()),
		ID:                              []byte{1, 2, 3, 4},
		Round:                           1,
		Height:                          1,
		LastPreparedRound:               1,
		LastPreparedValue:               []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: TestingSignedMsg,
		Decided:                         false,
		DecidedValue:                    []byte{1, 2, 3, 4},

		ProposeContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*spectypes.SignedSSVMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		PrepareContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*spectypes.SignedSSVMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		CommitContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*spectypes.SignedSSVMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		RoundChangeContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*spectypes.SignedSSVMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
	},
}

var TestingControllerStruct = &specqbft.Controller{
	Identifier:      []byte{1, 2, 3, 4},
	Height:          specqbft.Height(1),
	CommitteeMember: testingutils.TestingCommitteeMember(testingutils.Testing4SharesSet()),
	StoredInstances: specqbft.InstanceContainer{TestingInstanceStruct},
}
