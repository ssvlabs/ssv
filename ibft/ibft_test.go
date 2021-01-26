package ibft

import (
	"sync"
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/bloxapp/ssv/ibft/networker"
	"github.com/bloxapp/ssv/ibft/types"
)

type LocalNodeNetworker struct {
	t         *testing.T
	pipelines map[types.RoundState]map[string][]networker.PipelineFunc
	l         map[string]*sync.Mutex
}

func (n *LocalNodeNetworker) SetMessagePipeline(id string, roundState types.RoundState, pipeline []networker.PipelineFunc) {
	if n.pipelines[roundState] == nil {
		n.pipelines[roundState] = make(map[string][]networker.PipelineFunc)
	}
	n.pipelines[roundState][id] = pipeline
	n.l[id] = &sync.Mutex{}
}

func (n *LocalNodeNetworker) Broadcast(signed *types.SignedMessage) error {
	go func() {
		for id, pipelineForType := range n.pipelines[signed.Message.Type] {
			n.l[id].Lock()
			for _, item := range pipelineForType {
				err := item(signed)
				if err != nil {
					n.t.Errorf("failed to execute pipeline for node id %s", id)
					n.l[id].Unlock()
					break
				}
			}
			n.l[id].Unlock()
		}
	}()

	return nil
}

func generateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*types.Node) {
	bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*types.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &types.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func TestIBFTInstance_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	net := &LocalNodeNetworker{
		t:         t,
		pipelines: make(map[types.RoundState]map[string][]networker.PipelineFunc),
		l:         make(map[string]*sync.Mutex),
	}
	instances := make([]*iBFTInstance, 0)
	sks, nodes := generateNodes(4)
	params := &types.InstanceParams{
		ConsensusParams: types.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	leader := params.CommitteeSize() - 1
	for i := 0; i < params.CommitteeSize(); i++ {
		me := &types.Node{
			IbftId: uint64(i),
			Pk:     nodes[uint64(i)].Pk,
			Sk:     sks[uint64(i)].Serialize(),
		}
		instances = append(instances, New(logger, me, net, &day_number_consensus.DayNumberConsensus{Id: uint64(i), Leader: uint64(leader)}, params))
		instances[i].StartEventLoopAndMessagePipeline()
	}

	for _, i := range instances {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Second * 5)
}
