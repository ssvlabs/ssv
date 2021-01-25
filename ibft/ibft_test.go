package ibft

import (
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/bloxapp/ssv/ibft/types"
)

type LocalNodeNetworker struct {
	pipelines map[types.RoundState]map[string][]types.PipelineFunc
	l         map[string]*sync.Mutex
}

func (n *LocalNodeNetworker) SetMessagePipeline(id string, roundState types.RoundState, pipeline []types.PipelineFunc) {
	if n.pipelines[roundState] == nil {
		n.pipelines[roundState] = make(map[string][]types.PipelineFunc)
	}
	n.pipelines[roundState][id] = pipeline
	n.l[id] = &sync.Mutex{}
}

func (n *LocalNodeNetworker) Broadcast(msg *types.Message) error {
	go func() {
		for id, pipelineForType := range n.pipelines[msg.Type] {
			n.l[id].Lock()
			signed := &types.SignedMessage{ // TODO - broadcast only signed messages
				Message:   msg,
				Signature: nil,
				IbftId:    0,
			}
			for _, item := range pipelineForType {
				err := item(signed)
				if err != nil {
					logrus.WithError(err).Errorf("failed to execute pipeline for node id %s", id)
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
	net := &LocalNodeNetworker{pipelines: make(map[types.RoundState]map[string][]types.PipelineFunc), l: make(map[string]*sync.Mutex)}
	instances := make([]*iBFTInstance, 0)
	_, nodes := generateNodes(4)
	params := &types.InstanceParams{
		ConsensusParams: types.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	leader := params.CommitteeSize() - 1
	for i := 0; i < params.CommitteeSize(); i++ {
		instances = append(instances, New(nodes[uint64(i)], net, &day_number_consensus.DayNumberConsensus{Id: uint64(i), Leader: uint64(leader)}, params))
		instances[i].StartEventLoopAndMessagePipeline()
	}

	for _, i := range instances {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Second * 5)
}
