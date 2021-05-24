package spectesting

import (
	"encoding/json"
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

// PrePrepareMsg constructs and signs a pre-prepare msg
func PrePrepareMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// PrepareMsg constructs and signs a prepare msg
func PrepareMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// CommitMsg constructs and signs a commit msg
func CommitMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// ChangeRoundMsg constructs and signs a change round msg
func ChangeRoundMsg(t *testing.T, sk, lambda, prevLambda []byte, round, id uint64) *proto.SignedMessage {
	return ChangeRoundMsgWithPrepared(t, sk, lambda, prevLambda, nil, nil, round, 0, id)
}

// ChangeRoundMsgWithPrepared constructs and signs a change round msg
func ChangeRoundMsgWithPrepared(t *testing.T, sk, lambda, prevLambda, preparedValue []byte, signers map[uint64][]byte, round, preparedRound, id uint64) *proto.SignedMessage {
	crData := &proto.ChangeRoundData{
		PreparedRound:    preparedRound,
		PreparedValue:    preparedValue,
		JustificationMsg: nil,
		JustificationSig: nil,
		SignerIds:        nil,
	}

	if preparedValue != nil {
		crData.JustificationMsg = &proto.Message{
			Type:   proto.RoundState_Prepare,
			Round:  preparedRound,
			Lambda: lambda,
			Value:  preparedValue,
		}
		var aggregatedSig *bls.Sign
		SignerIds := make([]uint64, 0)
		for signerID, skByts := range signers {
			SignerIds = append(SignerIds, signerID)
			signed := SignMsg(t, signerID, skByts, crData.JustificationMsg)
			sig := &bls.Sign{}
			require.NoError(t, sig.Deserialize(signed.Signature))

			if aggregatedSig == nil {
				aggregatedSig = sig
			} else {
				aggregatedSig.Add(sig)
			}
		}
		crData.JustificationSig = aggregatedSig.Serialize()
		crData.SignerIds = SignerIds
	}

	byts, err := json.Marshal(crData)
	require.NoError(t, err)

	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_ChangeRound,
		Round:  round,
		Lambda: lambda,
		Value:  byts,
	})
}

// TestIBFTInstance returns a test iBFT instance
func TestIBFTInstance(t *testing.T, lambda []byte, prevLambda []byte) *ibft.Instance {
	opts := ibft.InstanceOptions{
		Logger:         zaptest.NewLogger(t),
		Me:             TestNodes()[1],
		Network:        local.NewLocalNetwork(),
		Queue:          msgqueue.New(),
		ValueCheck:     bytesval.New(TestInputValue()),
		LeaderSelector: &leader.Constant{LeaderIndex: 1},
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   TestNodes(),
		},
		Lambda:      lambda,
		ValidatorPK: fixtures.RefPk, // just as a value
	}

	return ibft.NewInstance(opts)
}

// TestNodes generates test nodes for SSV
func TestNodes() map[uint64]*proto.Node {
	return map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     TestPKs()[0],
			Sk:     TestSKs()[0],
		},
		2: {
			IbftId: 2,
			Pk:     TestPKs()[1],
			Sk:     TestSKs()[1],
		},
		3: {
			IbftId: 3,
			Pk:     TestPKs()[2],
			Sk:     TestSKs()[2],
		},
		4: {
			IbftId: 4,
			Pk:     TestPKs()[3],
			Sk:     TestSKs()[3],
		},
	}
}

// TestPKs PKS for TestSKs
func TestPKs() [][]byte {
	return [][]byte{
		fixtures.RefSplitSharesPubKeys[0],
		fixtures.RefSplitSharesPubKeys[1],
		fixtures.RefSplitSharesPubKeys[2],
		fixtures.RefSplitSharesPubKeys[3],
	}
}

// TestSKs returns reference test shares for SSV
func TestSKs() [][]byte {
	return [][]byte{
		fixtures.RefSplitShares[0],
		fixtures.RefSplitShares[1],
		fixtures.RefSplitShares[2],
		fixtures.RefSplitShares[3],
	}
}

// TestInputValue a const input test value
func TestInputValue() []byte {
	return []byte("testing value")
}

// SimulateTimeout simulates instance timeout
func SimulateTimeout(instance *ibft.Instance, toRound uint64) {
	instance.BumpRound(toRound)
	instance.SetStage(proto.RoundState_ChangeRound)
}

// RequireProcessedMessage will call ProcessMessage and verifies it returns true and nil for execution
func RequireProcessedMessage(t *testing.T, f func() (bool, error)) {
	res, err := f()
	require.NoError(t, err)
	require.True(t, res)
}

// RequireNotProcessedMessage will call ProcessMessage and verifies it returns false and nil for execution
func RequireNotProcessedMessage(t *testing.T, f func() (bool, error)) {
	res, err := f()
	require.NoError(t, err)
	require.False(t, res)
}

// RequireProcessMessageError will call ProcessMessage and verifies it returns true and error for execution
func RequireProcessMessageError(t *testing.T, f func() (bool, error), errStr string) {
	res, err := f()
	require.EqualError(t, err, errStr)
	require.True(t, res)
}
