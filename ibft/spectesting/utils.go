package spectesting

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/leader"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"go.uber.org/zap/zaptest"
	"testing"
)

func PrePrepareMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:           proto.RoundState_PrePrepare,
		Round:          round,
		Lambda:         lambda,
		PreviousLambda: prevLambda,
		Value:          inputValue,
	})
}

func PrepareMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:           proto.RoundState_Prepare,
		Round:          round,
		Lambda:         lambda,
		PreviousLambda: prevLambda,
		Value:          inputValue,
	})
}

func CommitMsg(t *testing.T, sk, lambda, prevLambda, inputValue []byte, round, id uint64) *proto.SignedMessage {
	return SignMsg(t, id, sk, &proto.Message{
		Type:           proto.RoundState_Commit,
		Round:          round,
		Lambda:         lambda,
		PreviousLambda: prevLambda,
		Value:          inputValue,
	})
}

func TestIBFTInstance(t *testing.T, lambda []byte, prevLambda []byte) *ibft.Instance {
	opts := ibft.InstanceOptions{
		Logger:         zaptest.NewLogger(t),
		Me:             TestNodes()[1],
		Network:        local.NewLocalNetwork(),
		Queue:          msgqueue.New(),
		Consensus:      bytesval.New(TestInputValue()),
		LeaderSelector: &leader.Constant{LeaderIndex: 1},
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   TestNodes(),
		},
		Lambda:         lambda,
		PreviousLambda: prevLambda,
	}

	return ibft.NewInstance(opts)
}

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

func TestPKs() [][]byte {
	return [][]byte{
		fixtures.RefSplitSharesPubKeys[0],
		fixtures.RefSplitSharesPubKeys[1],
		fixtures.RefSplitSharesPubKeys[2],
		fixtures.RefSplitSharesPubKeys[3],
	}
}

func TestSKs() [][]byte {
	return [][]byte{
		fixtures.RefSplitShares[0],
		fixtures.RefSplitShares[1],
		fixtures.RefSplitShares[2],
		fixtures.RefSplitShares[3],
	}
}

func TestInputValue() []byte {
	return []byte("testing value")
}
