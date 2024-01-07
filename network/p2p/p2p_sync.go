package p2pv1

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2p_protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multistream"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

// RegisterHandlers registers the given handlers
func (n *p2pNetwork) RegisterHandlers(logger *zap.Logger, handlers ...*p2pprotocol.SyncHandler) {
	m := make(map[libp2p_protocol.ID][]p2pprotocol.RequestHandler)
	for _, handler := range handlers {
		pid, _ := commons.ProtocolID(handler.Protocol)
		current, ok := m[pid]
		if !ok {
			current = make([]p2pprotocol.RequestHandler, 0)
		}
		current = append(current, handler.Handler)
		m[pid] = current
	}

	for pid, phandlers := range m {
		n.registerHandlers(logger, pid, phandlers...)
	}
}

func (n *p2pNetwork) registerHandlers(logger *zap.Logger, pid libp2p_protocol.ID, handlers ...p2pprotocol.RequestHandler) {
	handler := p2pprotocol.CombineRequestHandlers(handlers...)
	streamHandler := n.handleStream(logger, handler)
	n.host.SetStreamHandler(pid, func(stream libp2pnetwork.Stream) {
		err := streamHandler(stream)
		if err != nil {
			logger.Debug("stream handler failed", zap.Error(err))
		}
	})
}

func (n *p2pNetwork) handleStream(logger *zap.Logger, handler p2pprotocol.RequestHandler) func(stream libp2pnetwork.Stream) error {
	return func(stream libp2pnetwork.Stream) error {
		req, respond, done, err := n.streamCtrl.HandleStream(logger, stream)
		defer done()

		if err != nil {
			return errors.Wrap(err, "could not handle stream")
		}

		smsg, err := commons.DecodeNetworkMsg(req)
		if err != nil {
			return errors.Wrap(err, "could not decode msg from stream")
		}

		result, err := handler(smsg)
		if err != nil {
			return errors.Wrap(err, "could not handle msg from stream")
		}

		resultBytes, err := commons.EncodeNetworkMsg(result)
		if err != nil {
			return errors.Wrap(err, "could not encode msg")
		}

		if err := respond(resultBytes); err != nil {
			return errors.Wrap(err, "could not respond to stream")
		}

		return nil
	}
}

// getSubsetOfPeers returns a subset of the peers from that topic
func (n *p2pNetwork) getSubsetOfPeers(logger *zap.Logger, vpk spectypes.ValidatorPK, maxPeers int, filter func(peer.ID) bool) (peers []peer.ID, err error) {
	var ps []peer.ID
	seen := make(map[peer.ID]struct{})
	topics := commons.ValidatorTopicID(vpk)
	for _, topic := range topics {
		ps, err = n.topicsCtrl.Peers(topic)
		if err != nil {
			continue
		}
		for _, p := range ps {
			if _, ok := seen[p]; !ok && filter(p) {
				peers = append(peers, p)
				seen[p] = struct{}{}
			}
		}
	}
	// if we seen some peers, ignore the error
	if err != nil && len(seen) == 0 {
		return nil, errors.Wrapf(err, "could not read peers for validator %s", hex.EncodeToString(vpk))
	}
	if len(peers) == 0 {
		return nil, nil
	}
	if maxPeers > len(peers) {
		maxPeers = len(peers)
	} else {
		rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	}
	return peers[:maxPeers], nil
}

func (n *p2pNetwork) makeSyncRequest(logger *zap.Logger, peers []peer.ID, mid spectypes.MessageID, protocol libp2p_protocol.ID, syncMsg *message.SyncMessage) ([]p2pprotocol.SyncResult, error) {
	var results []p2pprotocol.SyncResult
	data, err := syncMsg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode sync message")
	}

	msg := &spectypes.SSVMessage{
		MsgType: message.SSVSyncMsgType,
		MsgID:   mid,
		Data:    data,
	}
	encoded, err := commons.EncodeNetworkMsg(msg)
	if err != nil {
		return nil, err
	}

	logger = logger.With(zap.String("protocol", string(protocol)))
	msgID := commons.MsgID()
	distinct := make(map[string]struct{})
	for _, pid := range peers {
		logger := logger.With(fields.PeerID(pid))

		raw, err := n.streamCtrl.Request(logger, pid, protocol, encoded)
		if err != nil {
			// TODO: is this how to check for ErrNotSupported?
			var e multistream.ErrNotSupported[libp2p_protocol.ID]
			if !errors.Is(err, e) {
				logger.Debug("could not make stream request", zap.Error(err))
			}
			continue
		}

		if n.cfg.Network.Beacon.EstimatedCurrentEpoch() > n.cfg.Network.PermissionlessActivationEpoch {
			decodedMsg, _, _, err := commons.DecodeSignedSSVMessage(raw)
			if err != nil {
				logger.Debug("could not decode signed SSV message", zap.Error(err))
			} else {
				raw = decodedMsg
			}
		}

		mid := msgID(raw)
		if _, ok := distinct[mid]; ok {
			continue
		}
		distinct[mid] = struct{}{}

		res, err := commons.DecodeNetworkMsg(raw)
		if err != nil {
			logger.Debug("could not decode stream response", zap.Error(err))
			continue
		}

		results = append(results, p2pprotocol.SyncResult{
			Msg:    res,
			Sender: pid.String(),
		})
	}
	return results, nil
}

// peersWithProtocolsFilter is used to accept peers that supports the given protocols
//
//nolint:unused
func (n *p2pNetwork) peersWithProtocolsFilter(protocols ...libp2p_protocol.ID) func(peer.ID) bool {
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

func waitSubsetOfPeers(
	logger *zap.Logger,
	getSubsetOfPeers func(logger *zap.Logger, vpk spectypes.ValidatorPK, maxPeers int, filter func(peer.ID) bool) (peers []peer.ID, err error),
	vpk spectypes.ValidatorPK,
	minPeers, maxPeers int,
	timeout time.Duration,
	filter func(peer.ID) bool,
) ([]peer.ID, error) {
	if minPeers > maxPeers {
		return nil, fmt.Errorf("minPeers should not be greater than maxPeers")
	}
	if minPeers < 0 || maxPeers < 0 {
		return nil, fmt.Errorf("minPeers and maxPeers should not be negative")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout should be positive")
	}

	// Wait for minPeers with a deadline.
	deadline := time.Now().Add(timeout)
	for {
		peers, err := getSubsetOfPeers(logger, vpk, maxPeers, filter)
		if err != nil {
			return nil, err
		}
		if len(peers) >= minPeers {
			// Found enough peers.
			return peers, nil
		}
		if time.Now().After(deadline) {
			// Timeout.
			return peers, nil
		}

		// Wait for a bit before trying again.
		time.Sleep(timeout/3 - timeout/10) // 3 retries with 10% margin.
	}
}
