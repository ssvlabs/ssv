package ibft

import (
	"sync"
	"testing"

	"github.com/bloxapp/ssv/ibft/types"
	"github.com/bloxapp/ssv/networker"
)

type LocalNodeNetworker struct {
	t         *testing.T
	replay    *IBFTReplay
	pipelines map[types.RoundState]map[uint64][]networker.PipelineFunc
	l         map[uint64]*sync.Mutex
}

func (n *LocalNodeNetworker) SetMessagePipeline(id uint64, roundState types.RoundState, pipeline []networker.PipelineFunc) {
	if n.pipelines[roundState] == nil {
		n.pipelines[roundState] = make(map[uint64][]networker.PipelineFunc)
	}
	n.pipelines[roundState][id] = pipeline
	n.l[id] = &sync.Mutex{}
}

func (n *LocalNodeNetworker) Broadcast(signed *types.SignedMessage) error {
	go func() {
		// verify node is not prevented from sending msgs
		if !n.replay.CanSend(signed.Message.Type, signed.Message.Round, signed.IbftId) {
			return
		}

		for id, pipelineForType := range n.pipelines[signed.Message.Type] {
			// verify node is not prevented from receiving msgs
			if !n.replay.CanReceive(signed.Message.Type, signed.Message.Round, id) {
				continue
			}

			n.l[id].Lock()
			for _, item := range pipelineForType {
				err := item(signed)
				if err != nil {
					n.t.Errorf("failed to execute pipeline for node id %d - %s", id, err)
					break
				}
			}
			n.l[id].Unlock()
		}
	}()

	return nil
}

// IBFTReplay allows to script a precise scenario for every ibft node's behaviour each round
type IBFTReplay struct {
	Networker *LocalNodeNetworker
	scripts   map[uint64]*RoundScript
	nodes     []uint64
}

func NewIBFTReplay(nodes map[uint64]*types.Node) *IBFTReplay {
	ret := &IBFTReplay{
		Networker: &LocalNodeNetworker{
			pipelines: make(map[types.RoundState]map[uint64][]networker.PipelineFunc),
			l:         make(map[uint64]*sync.Mutex),
		},
		scripts: make(map[uint64]*RoundScript),
		nodes:   make([]uint64, len(nodes)),
	}
	ret.Networker.replay = ret

	// set ids
	for k, v := range nodes {
		ret.nodes[k] = v.IbftId
	}

	return ret
}

func (r *IBFTReplay) StartRound(round uint64) *RoundScript {
	r.scripts[round] = NewRoundScript(r, r.nodes)
	return r.scripts[round]
}

func (r *IBFTReplay) CanSend(state types.RoundState, round uint64, node uint64) bool {
	if v, ok := r.scripts[round]; ok {
		return v.CanSend(state, node)
	}
	return true
}

func (r *IBFTReplay) CanReceive(state types.RoundState, round uint64, node uint64) bool {
	if v, ok := r.scripts[round]; ok {
		return v.CanSend(state, node)
	}
	return true
}

type RoundScript struct {
	replay *IBFTReplay
	rules  map[types.RoundState]map[uint64]bool // if true the node receives (and sends) all messages. False it doesn't
}

func NewRoundScript(r *IBFTReplay, nodes []uint64) *RoundScript {
	rules := make(map[types.RoundState]map[uint64]bool)
	for _, t := range []types.RoundState{types.RoundState_PrePrepare, types.RoundState_Prepare, types.RoundState_Commit, types.RoundState_ChangeRound} {
		rules[t] = make(map[uint64]bool)
		for _, id := range nodes {
			rules[t][id] = true
		}
	}
	return &RoundScript{
		rules:  rules,
		replay: r,
	}
}

func (r *RoundScript) CanSend(state types.RoundState, node uint64) bool {
	return r.rules[state][node]
}

func (r *RoundScript) CanReceive(state types.RoundState, node uint64) bool {
	return r.rules[state][node]
}

func (r *RoundScript) PreventMessages(state types.RoundState, nodes []uint64) *RoundScript {
	for _, id := range nodes {
		r.rules[state][id] = false
	}
	return r
}

func (r *RoundScript) EndRound() *IBFTReplay {
	return r.replay
}
