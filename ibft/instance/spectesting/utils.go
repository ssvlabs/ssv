package spectesting

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/beacon"
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	v0 "github.com/bloxapp/ssv/ibft/instance/forks/v0"
	"github.com/bloxapp/ssv/ibft/leader/constant"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/bloxapp/ssv/validator/storage"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

var (
	// RefPk is a testing PK of RefSk
	refPk = _byteArray("a9cf360aa15fb1d1d30ee2b578dc5884823c19661886ae8b892775ccb3bd96b7d7345569a2aa0b14e4d015c54a6a0c54")

	// RefSplitShares is RefSk split into 4 shares
	refSplitShares = [][]byte{ // sk split to 4: 2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c
		_byteArray("1a1b411e54ebb0973dc0f133c8b192cc4320fd464cbdcfe3be38b77f821f30bc"),
		_byteArray("6a93d37661cfe9cbaff9f051f2dd1d1995905932375e09357be1a50f7f4de323"),
		_byteArray("3596a78e633ad5071c0a77bb16b1a391b21ab47fb32ba1ba442a48e89ae11f9f"),
		_byteArray("62ff0c0cac676cd9e866377f4772d63f403b5734c02351701712a308d4d8e632"),
	}
	// RefSplitSharesPubKeys is the PK for RefSplitShares
	refSplitSharesPubKeys = [][]byte{
		_byteArray("84d90424a5511e3741ac3c99ee1dba39007a290410e805049d0ae40cde74191d785d7848f08b2dfb99b742ebfe846e3b"),
		_byteArray("b6ac738a09a6b7f3fb4f85bac26d8965f6329d431f484e8b43633f7b7e9afce0085bb592ea90df6176b2f2bd97dfd7f3"),
		_byteArray("a261c25548320f1aabfc2aac5da3737a0b8bbc992a5f4f937259d22d39fbf6ebf8ec561720de3a04f661c9772fcace96"),
		_byteArray("85dd2d89a3e320995507c46320f371dc85eb16f349d1c56d71b58663b5b6a5fd390fcf41cf9098471eb5437fd95be1ac"),
	}
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
func TestSharesAndSigner() (map[uint64]*storage.Share, beacon.KeyManager) {
	shares := map[uint64]*storage.Share{
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
	if err := ret.Deserialize(refPk); err != nil {
		panic(err)
	}
	return ret
}

// TestPKs PKS for TestSKs
func TestPKs() [][]byte {
	return [][]byte{
		refSplitSharesPubKeys[0],
		refSplitSharesPubKeys[1],
		refSplitSharesPubKeys[2],
		refSplitSharesPubKeys[3],
	}
}

// TestSKs returns reference test shares for SSV
func TestSKs() []*bls.SecretKey {
	threshold.Init()
	ret := make([]*bls.SecretKey, 4)
	for i, skByts := range [][]byte{
		refSplitShares[0],
		refSplitShares[1],
		refSplitShares[2],
		refSplitShares[3],
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
