package controller

import (
	"crypto/rsa"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
)

var testingMessage = &specqbft.Message{
	MsgType:    specqbft.ProposalMsgType,
	Height:     specqbft.FirstHeight,
	Round:      specqbft.FirstRound,
	Identifier: []byte{1, 2, 3, 4},
	Root:       [32]byte{1, 2, 3, 4},
}

var testingSignedMsg = func() *specqbft.ProcessingMessage {
	return testingutils.ToProcessingMessage(testingutils.SignQBFTMsg(testingSK, 1, testingMessage))
}()

var testingSK = func() *rsa.PrivateKey {
	skPem, _, _ := spectypes.GenerateKey()
	ret, _ := spectypes.PemToPrivateKey(skPem)
	return ret
}()

var testingInstanceStruct = &specqbft.Instance{
	State: &specqbft.State{
		CommitteeMember:                 testingutils.TestingCommitteeMember(testingutils.Testing4SharesSet()),
		ID:                              []byte{1, 2, 3, 4},
		Round:                           1,
		Height:                          1,
		LastPreparedRound:               1,
		LastPreparedValue:               []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: testingSignedMsg,
		Decided:                         false,
		DecidedValue:                    []byte{1, 2, 3, 4},

		ProposeContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		PrepareContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		CommitContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		RoundChangeContainer: &specqbft.MsgContainer{
			Msgs: map[specqbft.Round][]*specqbft.ProcessingMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
	},
}

var testingControllerStruct = &specqbft.Controller{
	Identifier:      []byte{1, 2, 3, 4},
	Height:          specqbft.Height(1),
	CommitteeMember: testingutils.TestingCommitteeMember(testingutils.Testing4SharesSet()),
	StoredInstances: specqbft.InstanceContainer{testingInstanceStruct},
}
