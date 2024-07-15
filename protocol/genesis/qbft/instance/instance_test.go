package instance

import (
	"testing"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	"github.com/stretchr/testify/require"
)

func TestInstance_Marshaling(t *testing.T) {
	var TestingMessage = &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     specqbft.FirstHeight,
		Round:      specqbft.FirstRound,
		Identifier: []byte{1, 2, 3, 4},
		Root:       testingutils.TestingQBFTRootData,
	}
	TestingSK := func() *bls.SecretKey {
		spectypes.InitBLS()
		ret := &bls.SecretKey{}
		ret.SetByCSPRNG()
		return ret
	}()
	testingSignedMsg := func() *specqbft.SignedMessage {
		return testingutils.SignQBFTMsg(TestingSK, 1, TestingMessage)
	}()
	testingValidatorPK := spec.BLSPubKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}
	testingShare := &spectypes.Share{
		OperatorID:      1,
		ValidatorPubKey: testingValidatorPK[:],
		SharePubKey:     TestingSK.GetPublicKey().Serialize(),
		DomainType:      spectypes.PrimusTestnet,
		Quorum:          3,
		PartialQuorum:   2,
		Committee: []*spectypes.Operator{
			{
				OperatorID: 1,
				PubKey:     TestingSK.GetPublicKey().Serialize(),
			},
		},
	}
	i := &specqbft.Instance{
		State: &specqbft.State{
			Share:                           testingShare,
			ID:                              []byte{1, 2, 3, 4},
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte{1, 2, 3, 4},
			ProposalAcceptedForCurrentRound: testingSignedMsg,
			Decided:                         false,
			DecidedValue:                    []byte{1, 2, 3, 4},

			ProposeContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			PrepareContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			CommitContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
			RoundChangeContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
					1: {
						testingSignedMsg,
					},
				},
			},
		},
	}

	byts, err := i.Encode()
	require.NoError(t, err)

	decoded := &specqbft.Instance{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
