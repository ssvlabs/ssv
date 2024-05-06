package qbft

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var TestingMessage = &genesisspecqbft.Message{
	MsgType:    genesisspecqbft.ProposalMsgType,
	Height:     genesisspecqbft.FirstHeight,
	Round:      genesisspecqbft.FirstRound,
	Identifier: []byte{1, 2, 3, 4},
	Root:       [32]byte{1, 2, 3, 4},
}

var TestingSignedMsg = func() *genesisspecqbft.SignedMessage {
	return SignMsg(TestingSK, 1, TestingMessage)
}()

var SignMsg = func(sk *bls.SecretKey, id types.OperatorID, msg *genesisspecqbft.Message) *genesisspecqbft.SignedMessage {
	domain := testingutils.TestingSSVDomainType
	sigType := types.QBFTSignatureType

	r, _ := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	sig := sk.SignByte(r[:])

	return &genesisspecqbft.SignedMessage{
		Message:   *msg,
		Signers:   []types.OperatorID{id},
		Signature: sig.Serialize(),
	}
}

var TestingSK = func() *bls.SecretKey {
	types.InitBLS()
	ret := &bls.SecretKey{}
	ret.SetByCSPRNG()
	return ret
}()

var testingValidatorPK = phase0.BLSPubKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}

var testingShare = &types.Share{
	OperatorID:      1,
	ValidatorPubKey: testingValidatorPK[:],
	SharePubKey:     TestingSK.GetPublicKey().Serialize(),
	DomainType:      testingutils.TestingSSVDomainType,
	Quorum:          3,
	PartialQuorum:   2,
	Committee: []*types.Operator{
		{
			OperatorID:  1,
			SharePubKey: TestingSK.GetPublicKey().Serialize(),
		},
	},
}

var TestingInstanceStruct = &genesisspecqbft.Instance{
	State: &genesisspecqbft.State{
		Share:                           testingShare,
		ID:                              []byte{1, 2, 3, 4},
		Round:                           1,
		Height:                          1,
		LastPreparedRound:               1,
		LastPreparedValue:               []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: TestingSignedMsg,
		Decided:                         false,
		DecidedValue:                    []byte{1, 2, 3, 4},

		ProposeContainer: &genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		PrepareContainer: &genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		CommitContainer: &genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
		RoundChangeContainer: &genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
				1: {
					TestingSignedMsg,
				},
			},
		},
	},
}

var TestingControllerStruct = &genesisspecqbft.Controller{
	Identifier:      []byte{1, 2, 3, 4},
	Height:          genesisspecqbft.Height(1),
	Share:           testingShare,
	StoredInstances: genesisspecqbft.InstanceContainer{TestingInstanceStruct},
}
