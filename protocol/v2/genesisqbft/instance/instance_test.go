package instance

import (
	"testing"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
)

func TestInstance_Marshaling(t *testing.T) {
	var TestingMessage = &genesisspecqbft.Message{
		MsgType:    genesisspecqbft.ProposalMsgType,
		Height:     genesisspecqbft.FirstHeight,
		Round:      genesisspecqbft.FirstRound,
		Identifier: []byte{1, 2, 3, 4},
		Root:       testingutils.TestingQBFTRootData,
	}
	TestingSK := func() *bls.SecretKey {
		genesisspectypes.InitBLS()
		ret := &bls.SecretKey{}
		ret.SetByCSPRNG()
		return ret
	}()
	testingSignedMsg := func() *genesisspecqbft.SignedMessage {
		return testingutils.SignQBFTMsg(TestingSK, 1, TestingMessage)
	}()
	testingValidatorPK := spec.BLSPubKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	testingShare := &genesisspectypes.Share{
		OperatorID:      1,
		ValidatorPubKey: testingValidatorPK[:],
		SharePubKey:     TestingSK.GetPublicKey().Serialize(),
		DomainType:      genesisspectypes.PrimusTestnet,
		Quorum:          3,
		PartialQuorum:   2,
		Committee: []*genesisspectypes.Operator{
			{
				OperatorID:  1,
				SharePubKey: TestingSK.GetPublicKey().Serialize(),
			},
		},
	}
	i := &genesisspecqbft.Instance{
		State: &genesisspecqbft.State{
			Share:                           testingShare,
			ID:                              []byte{1, 2, 3, 4},
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte{1, 2, 3, 4},
			ProposalAcceptedForCurrentRound: testingSignedMsg,
			Decided:                         false,
			DecidedValue:                    []byte{1, 2, 3, 4},

			ProposeContainer: &genesisspecqbft.MsgContainer{
				Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			PrepareContainer: &genesisspecqbft.MsgContainer{
				Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			CommitContainer: &genesisspecqbft.MsgContainer{
				Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			RoundChangeContainer: &genesisspecqbft.MsgContainer{
				Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
		},
	}

	byts, err := i.Encode()
	require.NoError(t, err)

	decoded := &genesisspecqbft.Instance{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
