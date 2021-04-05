package local

import (
	"sync"

	"github.com/bloxapp/ssv/ibft/proto"
)

// Local implements network.Local interface
type Local struct {
	msgC               []chan *proto.SignedMessage
	sigC               []chan *proto.SignedMessage
	createChannelMutex sync.Mutex
}

func NewLocalNetwork() *Local {
	return &Local{
		msgC: make([]chan *proto.SignedMessage, 0),
		sigC: make([]chan *proto.SignedMessage, 0),
	}
}

// ReceivedMsgChan implements network.Local interface
func (n *Local) ReceivedMsgChan() <-chan *proto.SignedMessage {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.msgC = append(n.msgC, c)
	return c
}

// ReceivedSignatureChan returns the channel with signatures
func (n *Local) ReceivedSignatureChan() <-chan *proto.SignedMessage {
	n.createChannelMutex.Lock()
	defer n.createChannelMutex.Unlock()
	c := make(chan *proto.SignedMessage)
	n.sigC = append(n.sigC, c)
	return c
}

// Broadcast implements network.Local interface
func (n *Local) Broadcast(signed *proto.SignedMessage) error {
	go func() {

		// verify node is not prevented from sending msgs
		//for _, id := range signed.SignerIds {
		//	if !n.replay.CanSend(signed.Message.Type, signed.Message.Lambda, signed.Message.Round, id) {
		//		return
		//	}
		//}

		for _, c := range n.msgC {
			//if !n.replay.CanReceive(signed.Message.Type, signed.Message.Lambda, signed.Message.Round, i) {
			//	fmt.Printf("can't receive, node %d, lambda %s\n", i, hex.EncodeToString(signed.Message.Lambda))
			//	continue
			//}

			c <- signed
		}
	}()

	return nil
}

// BroadcastSignature broadcasts the given signature for the given lambda
func (n *Local) BroadcastSignature(msg *proto.SignedMessage) error {
	go func() {
		for _, c := range n.sigC {
			c <- msg
		}
	}()
	return nil
}

//// Replay allows to script a precise scenario for every ibft node's behaviour each round
//type Replay struct {
//	Network *Local
//	Storage storage.Storage
//	scripts map[string]map[uint64]*RoundScript
//	nodes   []uint64
//}
//
//// NewReplay is the constructor of Replay
//func NewReplay(nodes map[uint64]*proto.Node) *Replay {
//	ret := &Replay{
//		Network: NewLocalNetwork(),
//		Storage: inmem.New(),
//		scripts: make(map[string]map[uint64]*RoundScript),
//		nodes:   make([]uint64, len(nodes)),
//	}
//	ret.Network.replay = ret
//
//	// set ids
//	for _, v := range nodes {
//		ret.nodes = append(ret.nodes, v.IbftId)
//	}
//
//	return ret
//}
//
//func (r *Replay) SetScript(identifier []byte, round uint64, script *RoundScript) {
//	if r.scripts[hex.EncodeToString(identifier)] == nil {
//		r.scripts[hex.EncodeToString(identifier)] = make(map[uint64]*RoundScript)
//	}
//	r.scripts[hex.EncodeToString(identifier)][round] = script
//}
//
//// StartRound starts the given round
//func (r *Replay) StartRound(identifier []byte, round uint64) *RoundScript {
//	r.scripts[hex.EncodeToString(identifier)][round] = NewRoundScript(r, r.nodes)
//	return r.scripts[hex.EncodeToString(identifier)][round]
//}
//
//// CanSend returns true if message can be sent
//func (r *Replay) CanSend(state proto.RoundState, identifier []byte, round uint64, node uint64) bool {
//	if v, ok := r.scripts[hex.EncodeToString(identifier)][round]; ok {
//		return v.CanSend(state, node)
//	}
//	return true
//}
//
//// CanReceive returns true if the message can be received
//func (r *Replay) CanReceive(state proto.RoundState, identifier []byte, round uint64, node uint64) bool {
//	if v, ok := r.scripts[hex.EncodeToString(identifier)][round]; ok {
//		return v.CanSend(state, node)
//	}
//	return true
//}
//
//// RoundScript ...
//type RoundScript struct {
//	replay *Replay
//	rules  map[proto.RoundState]map[uint64]bool // if true the node receives (and sends) all messages. False it doesn't
//}
//
//// NewRoundScript is the constructor of RoundScript
//func NewRoundScript(r *Replay, nodes []uint64) *RoundScript {
//	rules := make(map[proto.RoundState]map[uint64]bool)
//	for _, t := range []proto.RoundState{proto.RoundState_PrePrepare, proto.RoundState_Prepare, proto.RoundState_Commit, proto.RoundState_ChangeRound} {
//		rules[t] = make(map[uint64]bool)
//		for _, id := range nodes {
//			rules[t][id] = true
//		}
//	}
//	return &RoundScript{
//		rules:  rules,
//		replay: r,
//	}
//}
//
//// CanSend ...
//func (r *RoundScript) CanSend(state proto.RoundState, node uint64) bool {
//	return r.rules[state][node]
//}
//
//// CanReceive ...
//func (r *RoundScript) CanReceive(state proto.RoundState, node uint64) bool {
//	return r.rules[state][node]
//}
//
//// PreventMessages ...
//func (r *RoundScript) PreventMessages(state proto.RoundState, nodes []uint64) *RoundScript {
//	for _, id := range nodes {
//		r.rules[state][id] = false
//	}
//	return r
//}
//
//// EndRound ...
//func (r *RoundScript) EndRound() *Replay {
//	return r.replay
//}
