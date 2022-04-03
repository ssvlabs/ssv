package p2pv1

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocol_p2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	libp2p_protocol "github.com/libp2p/go-libp2p-core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	peersForSync = 10

	// TODO: add
	legacyMsgStream = "/sync/0.0.1"

	lastDecidedProtocol = "/ssv/sync/decided/last"
	changeRoundProtocol = "/ssv/sync/round"
	historyProtocol     = "/ssv/sync/decided/history"
)

// LastDecided fetches last decided from a random set of peers
func (n *p2pNetwork) LastDecided(mid message.Identifier) ([]*message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	peers, err := n.getSubsetOfPeers(mid.GetValidatorPK(), allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(peers, mid, lastDecidedProtocol, &message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
	})
}

// GetHistory sync the given range from a set of peers that supports history for the given identifier
func (n *p2pNetwork) GetHistory(mid message.Identifier, from, to message.Height) ([]*message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	peers, err := n.getSubsetOfPeers(mid.GetValidatorPK(), n.peersWithProtocolsFilter(historyProtocol))
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(peers, mid, historyProtocol, &message.SyncMessage{
		Params: &message.SyncParams{
			Height:     []message.Height{from, to},
			Identifier: mid,
		},
	})
}

// LastChangeRound fetches last change round message from a random set of peers
func (n *p2pNetwork) LastChangeRound(mid message.Identifier, height message.Height) ([]*message.SSVMessage, error) {
	if !n.isReady() {
		return nil, ErrNetworkIsNotReady
	}
	peers, err := n.getSubsetOfPeers(mid.GetValidatorPK(), allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(peers, mid, changeRoundProtocol, &message.SyncMessage{
		Params: &message.SyncParams{
			Height:     []message.Height{height},
			Identifier: mid,
		},
	})
}

func (n *p2pNetwork) setupLegacySyncHandler() error {
	n.RegisterHandler(legacyMsgStream, func(ssvMessage *message.SSVMessage) (*message.SSVMessage, error) {
		// TODO: implement
		return ssvMessage, nil
	})

	return nil
}

// RegisterHandler registers the given handler for the stream
func (n *p2pNetwork) RegisterHandler(pid string, handler protocol_p2p.RequestHandler) {
	n.host.SetStreamHandler(libp2p_protocol.ID(pid), func(stream libp2pnetwork.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		defer done()
		if err != nil {
			//n.logger.Warn("could not handle stream", zap.Error(err))
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
	})
}

// getSubsetOfPeers returns a subset of the peers from that topic
func (n *p2pNetwork) getSubsetOfPeers(vpk message.ValidatorPK, filter func(peer.ID) bool) ([]peer.ID, error) {
	var peers []peer.ID
	topics := n.fork.ValidatorTopicID(vpk)
	for _, topic := range topics {
		ps, err := n.topicsCtrl.Peers(topic)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			if filter(p) {
				peers = append(peers, p)
			}
		}
	}
	// TODO: shuffle peers
	return peers[:peersForSync], nil
}

func (n *p2pNetwork) makeSyncRequest(peers []peer.ID, mid message.Identifier, protocol string, syncMsg *message.SyncMessage) ([]*message.SSVMessage, error) {
	var results []*message.SSVMessage
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
		raw, err := n.streamCtrl.Request(pid, libp2p_protocol.ID(protocol), encoded)
		if err != nil {
			// TODO: log/trace error?
			continue
		}
		res, err := n.fork.DecodeNetworkMsg(raw)
		if err != nil {
			// TODO: log/trace error?
			continue
		}
		results = append(results, res.(*message.SSVMessage))
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
