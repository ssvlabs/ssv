package p2pv1

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	libp2p_protocol "github.com/libp2p/go-libp2p-core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// LastDecided fetches last decided from a random set of peers
func (n *p2pNetwork) LastDecided(mid message.Identifier) ([]protocolp2p.SyncResult, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	pid, peerCount := n.fork.ProtocolID(protocolp2p.LastDecidedProtocol)
	peers, err := n.getSubsetOfPeers(mid.GetValidatorPK(), peerCount, allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(peers, mid, pid, &message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
	})
}

// GetHistory sync the given range from a set of peers that supports history for the given identifier
func (n *p2pNetwork) GetHistory(mid message.Identifier, from, to message.Height, targets ...string) ([]protocolp2p.SyncResult, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	protocolID, peerCount := n.fork.ProtocolID(protocolp2p.DecidedHistoryProtocol)
	peers := make([]peer.ID, 0)
	for _, t := range targets {
		p, err := peer.Decode(t)
		if err != nil {
			continue
		}
		peers = append(peers, p)
	}
	// if no peers were provided -> select a random set of peers
	if len(peers) == 0 {
		random, err := n.getSubsetOfPeers(mid.GetValidatorPK(), peerCount, n.peersWithProtocolsFilter(string(protocolID)))
		if err != nil {
			return nil, errors.Wrap(err, "could not get subset of peers")
		}
		peers = random
	}
	maxBatchRes := message.Height(n.cfg.MaxBatchResponse)
	var results []protocolp2p.SyncResult
	for from < to {
		currentEnd := to
		if to-from > maxBatchRes {
			currentEnd = from + maxBatchRes
		}
		batchResults, err := n.makeSyncRequest(peers, mid, protocolID, &message.SyncMessage{
			Params: &message.SyncParams{
				Height:     []message.Height{from, currentEnd},
				Identifier: mid,
			},
			Protocol: message.DecidedHistoryType,
		})
		if err != nil {
			return results, err
		}
		results = append(results, batchResults...)
		from = currentEnd
	}
	return results, nil
}

// LastChangeRound fetches last change round message from a random set of peers
func (n *p2pNetwork) LastChangeRound(mid message.Identifier, height message.Height) ([]protocolp2p.SyncResult, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	pid, peerCount := n.fork.ProtocolID(protocolp2p.LastChangeRoundProtocol)
	peers, err := n.getSubsetOfPeers(mid.GetValidatorPK(), peerCount, allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(peers, mid, pid, &message.SyncMessage{
		Params: &message.SyncParams{
			Height:     []message.Height{height},
			Identifier: mid,
		},
		Protocol: message.LastChangeRoundType,
	})
}

// RegisterHandlers registers the given handlers
func (n *p2pNetwork) RegisterHandlers(handlers ...*protocolp2p.SyncHandler) {
	m := make(map[libp2p_protocol.ID][]protocolp2p.RequestHandler)
	for _, handler := range handlers {
		pid, _ := n.fork.ProtocolID(handler.Protocol)
		current, ok := m[pid]
		if !ok {
			current = make([]protocolp2p.RequestHandler, 0)
		}
		current = append(current, handler.Handler)
		m[pid] = current
	}

	for pid, phandlers := range m {
		n.registerHandlers(pid, phandlers...)
	}
}

func (n *p2pNetwork) registerHandlers(pid libp2p_protocol.ID, handlers ...protocolp2p.RequestHandler) {
	handler := protocolp2p.CombineRequestHandlers(handlers...)
	n.host.SetStreamHandler(pid, func(stream libp2pnetwork.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		defer done()
		if err != nil {
			n.logger.Warn("could not handle stream", zap.Error(err))
			return
		}
		msg, err := n.fork.DecodeNetworkMsg(req)
		if err != nil {
			n.logger.Warn("could not decode msg from stream", zap.Error(err))
			return
		}
		smsg, ok := msg.(*message.SSVMessage)
		if !ok {
			n.logger.Warn("could not cast msg from stream", zap.Error(err))
			return
		}
		result, err := handler(smsg)
		if err != nil {
			n.logger.Warn("could not handle msg from stream")
			return
		}
		resultBytes, err := n.fork.EncodeNetworkMsg(result)
		if err != nil {
			n.logger.Warn("could not encode msg", zap.Error(err))
			return
		}
		if err := respond(resultBytes); err != nil {
			n.logger.Warn("could not respond to stream", zap.Error(err))
			return
		}
		n.logger.Info("stream handler done")
	})
}

// getSubsetOfPeers returns a subset of the peers from that topic
func (n *p2pNetwork) getSubsetOfPeers(vpk message.ValidatorPK, peerCount int, filter func(peer.ID) bool) ([]peer.ID, error) {
	var peers []peer.ID
	seen := make(map[peer.ID]struct{})
	topics := n.fork.ValidatorTopicID(vpk)
	for _, topic := range topics {
		ps, err := n.topicsCtrl.Peers(topic)
		if err != nil {
			return nil, errors.Wrap(err, "could not read peers")
		}
		for _, p := range ps {
			if _, ok := seen[p]; !ok && filter(p) {
				peers = append(peers, p)
				seen[p] = struct{}{}
			}
		}
	}
	if len(peers) == 0 {
		n.logger.Debug("could not find peers", zap.Any("topics", topics))
		return nil, nil
	}
	// TODO: shuffle peers
	i := peerCount
	if i > len(peers) {
		i = len(peers)
	}
	return peers[:i], nil
}

func (n *p2pNetwork) makeSyncRequest(peers []peer.ID, mid message.Identifier, protocol libp2p_protocol.ID, syncMsg *message.SyncMessage) ([]protocolp2p.SyncResult, error) {
	var results []protocolp2p.SyncResult
	data, err := syncMsg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode sync message")
	}
	msg := &message.SSVMessage{
		MsgType: message.SSVSyncMsgType,
		ID:      mid,
		Data:    data,
	}
	encoded, err := n.fork.EncodeNetworkMsg(msg)
	if err != nil {
		return nil, err
	}
	for _, pid := range peers {
		raw, err := n.streamCtrl.Request(pid, protocol, encoded)
		if err != nil {
			// TODO: log/trace error?
			continue
		}
		res, err := n.fork.DecodeNetworkMsg(raw)
		if err != nil {
			// TODO: log/trace error?
			continue
		}
		results = append(results, protocolp2p.SyncResult{
			Msg:    res.(*message.SSVMessage),
			Sender: pid.String(),
		})
	}
	return results, nil
}

// peersWithProtocolsFilter is used to accept peers that supports the given protocols
func (n *p2pNetwork) peersWithProtocolsFilter(protocols ...string) func(peer.ID) bool {
	return func(id peer.ID) bool {
		supported, err := n.host.Network().Peerstore().SupportsProtocols(id, protocols...)
		if err != nil {
			// TODO: log/trace error
			return false
		}
		return len(supported) > 0
	}
}

// allPeersFilter is used to accept all peers in a given subnet
func allPeersFilter(id peer.ID) bool {
	return true
}
