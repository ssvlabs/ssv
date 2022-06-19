package qbft

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var TestingMessage = &Message{
	MsgType:    ProposalMsgType,
	Height:     FirstHeight,
	Round:      FirstRound,
	Identifier: []byte{1, 2, 3, 4},
	Data:       []byte{1, 2, 3, 4},
}
var testingSignedMsg = func() *SignedMessage {
	return SignMsg(TestingSK, 1, TestingMessage)
}()
var SignMsg = func(sk *bls.SecretKey, id types.OperatorID, msg *Message) *SignedMessage {
	domain := types.PrimusTestnet
	sigType := types.QBFTSignatureType

	r, _ := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	sig := sk.SignByte(r)

	return &SignedMessage{
		Message:   msg,
		Signers:   []types.OperatorID{id},
		Signature: sig.Serialize(),
	}
}
var TestingSK = func() *bls.SecretKey {
	threshold.Init()
	ret := &bls.SecretKey{}
	ret.SetByCSPRNG()
	return ret
}()
var testingValidatorPK = spec.BLSPubKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
var testingShare = &types.Share{
	OperatorID:      1,
	ValidatorPubKey: testingValidatorPK[:],
	SharePubKey:     TestingSK.GetPublicKey().Serialize(),
	DomainType:      types.PrimusTestnet,
	Quorum:          3,
	PartialQuorum:   2,
	Committee: []*types.Operator{
		{
			OperatorID: 1,
			PubKey:     TestingSK.GetPublicKey().Serialize(),
		},
	},
}
var testingInstanceStruct = &Instance{
	State: &State{
		Share:                           testingShare,
		ID:                              []byte{1, 2, 3, 4},
		Round:                           1,
		Height:                          1,
		LastPreparedRound:               1,
		LastPreparedValue:               []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: testingSignedMsg,
		Decided:                         false,
		DecidedValue:                    []byte{1, 2, 3, 4},

		ProposeContainer: &MsgContainer{
			Msgs: map[Round][]*SignedMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		PrepareContainer: &MsgContainer{
			Msgs: map[Round][]*SignedMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		CommitContainer: &MsgContainer{
			Msgs: map[Round][]*SignedMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
		RoundChangeContainer: &MsgContainer{
			Msgs: map[Round][]*SignedMessage{
				1: {
					testingSignedMsg,
				},
			},
		},
	},
}
var testingControllerStruct = &Controller{
	Identifier: []byte{1, 2, 3, 4},
	Height:     Height(1),
	Share:      testingShare,
	StoredInstances: [HistoricalInstanceCapacity]*Instance{
		testingInstanceStruct,
	},
}
