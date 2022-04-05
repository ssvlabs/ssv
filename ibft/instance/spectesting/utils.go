package spectesting

import (
	"encoding/json"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/fixtures"
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	v0 "github.com/bloxapp/ssv/ibft/instance/forks/v0"
	"github.com/bloxapp/ssv/ibft/leader/constant"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"testing"
)

// PrePrepareMsg constructs and signs a pre-prepare msg
func PrePrepareMsg(t *testing.T, sk *bls.SecretKey, lambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// PrepareMsg constructs and signs a prepare msg
func PrepareMsg(t *testing.T, sk *bls.SecretKey, lambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// CommitMsg constructs and signs a commit msg
func CommitMsg(t *testing.T, sk *bls.SecretKey, lambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:   proto.RoundState_Commit,
		Round:  round,
		Lambda: lambda,
		Value:  inputValue,
	})
}

// ChangeRoundMsg constructs and signs a change round msg
func ChangeRoundMsg(t *testing.T, sk *bls.SecretKey, lambda []byte, round, id uint64) *proto.SignedMessage {
	return ChangeRoundMsgWithPrepared(t, sk, lambda, nil, nil, round, 0, id)
}

// ChangeRoundMsgWithPrepared constructs and signs a change round msg
func ChangeRoundMsgWithPrepared(t *testing.T, sk *bls.SecretKey, lambda, preparedValue []byte, signers map[uint64]*bls.SecretKey, round, preparedRound, id uint64) *proto.SignedMessage {
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
func TestIBFTInstance(t *testing.T, lambda []byte) *ibft2.Instance {
	shares, km := TestSharesAndSigner()

	opts := &ibft2.InstanceOptions{
		Logger:         zaptest.NewLogger(t),
		ValidatorShare: shares[1],
		Network:        local.NewLocalNetwork(),
		Queue:          msgqueue.New(),
		ValueCheck:     bytesval.NewNotEqualBytes(InvalidTestInputValue()),
		Config:         proto.DefaultConsensusParams(),
		Lambda:         lambda,
		LeaderSelector: &constant.Constant{LeaderIndex: 0},
		Fork:           v0.New(),
		Signer:         km,
	}

	return ibft2.NewInstance(opts).(*ibft2.Instance)
}

// TestSharesAndSigner generates test nodes for SSV
func TestSharesAndSigner() (map[uint64]*keymanager.Share, beacon.KeyManager) {
	shares := map[uint64]*keymanager.Share{
		1: {
			NodeID:    1,
			PublicKey: TestValidatorPK(),
			Committee: TestNodes(),
		},
		2: {
			NodeID:    2,
			PublicKey: TestValidatorPK(),
			Committee: TestNodes(),
		},
		3: {
			NodeID:    3,
			PublicKey: TestValidatorPK(),
			Committee: TestNodes(),
		},
		4: {
			NodeID:    4,
			PublicKey: TestValidatorPK(),
			Committee: TestNodes(),
		},
	}

	km := newTestKM()
	if err := km.AddShare(TestSKs()[0]); err != nil {
		panic(err)
	}
	if err := km.AddShare(TestSKs()[1]); err != nil {
		panic(err)
	}
	if err := km.AddShare(TestSKs()[2]); err != nil {
		panic(err)
	}
	if err := km.AddShare(TestSKs()[3]); err != nil {
		panic(err)
	}
	return shares, km
}

// TestNodes generates test nodes for SSV
func TestNodes() map[uint64]*proto.Node {
	return map[uint64]*proto.Node{
		1: {
			IbftId: 1,
			Pk:     TestPKs()[0],
		},
		2: {
			IbftId: 2,
			Pk:     TestPKs()[1],
		},
		3: {
			IbftId: 3,
			Pk:     TestPKs()[2],
		},
		4: {
			IbftId: 4,
			Pk:     TestPKs()[3],
		},
	}
}

// TestValidatorPK returns ref validator pk
func TestValidatorPK() *bls.PublicKey {
	threshold.Init()
	ret := &bls.PublicKey{}
	if err := ret.Deserialize(fixtures.RefPk); err != nil {
		panic(err)
	}
	return ret
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
func TestSKs() []*bls.SecretKey {
	threshold.Init()
	ret := make([]*bls.SecretKey, 4)
	for i, skByts := range [][]byte{
		fixtures.RefSplitShares[0],
		fixtures.RefSplitShares[1],
		fixtures.RefSplitShares[2],
		fixtures.RefSplitShares[3],
	} {
		sk := &bls.SecretKey{}
		if err := sk.Deserialize(skByts); err != nil {
			panic(err)
		}
		ret[i] = sk
	}

	return ret
}

// TestInputValue a const input test value
func TestInputValue() []byte {
	return []byte("testing value")
}

// InvalidTestInputValue a const input invalid test value
func InvalidTestInputValue() []byte {
	return []byte("invalid testing value")
}

// SimulateTimeout simulates instance timeout
func SimulateTimeout(instance *ibft2.Instance, toRound uint64) {
	instance.BumpRound()
	instance.ProcessStageChange(proto.RoundState_ChangeRound)
}

// RequireReturnedTrueNoError will call ProcessMessage and verifies it returns true and nil for execution
func RequireReturnedTrueNoError(t *testing.T, f func() (bool, error)) {
	res, err := f()
	require.True(t, res)
	require.NoError(t, err)
}

// RequireReturnedFalseNoError will call ProcessMessage and verifies it returns false and nil for execution
func RequireReturnedFalseNoError(t *testing.T, f func() (bool, error)) {
	res, err := f()
	require.False(t, res)
	require.NoError(t, err)
}

// RequireReturnedTrueWithError will call ProcessMessage and verifies it returns true and error for execution
func RequireReturnedTrueWithError(t *testing.T, f func() (bool, error), errStr string) {
	res, err := f()
	require.True(t, res)
	require.EqualError(t, err, errStr)
}
