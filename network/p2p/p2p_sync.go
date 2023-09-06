package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"

	"github.com/multiformats/go-multistream"

	"github.com/bloxapp/ssv-spec/qbft"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2p_protocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

func (n *p2pNetwork) SyncHighestDecided(mid spectypes.MessageID) error {
	return n.syncer.SyncHighestDecided(context.Background(), n.interfaceLogger, mid, func(msg spectypes.SSVMessage) {
		n.msgRouter.Route(n.interfaceLogger, msg)
	})
}

func (n *p2pNetwork) SyncDecidedByRange(mid spectypes.MessageID, from, to qbft.Height) {
	if !n.cfg.FullNode {
		return
	}
	// TODO: uncomment to fix syncing bug!
	// if from < to {
	// 	n.logger.Warn("failed to sync decided by range: from is greater than to",
	// 		zap.String("pubkey", hex.EncodeToString(mid.GetPubKey())),
	// 		zap.String("role", mid.GetRoleType().String()),
	// 		zap.Uint64("from", uint64(from)),
	// 		zap.Uint64("to", uint64(to)))
	// 	return
	// }
	if to > from {
		n.interfaceLogger.Warn("failed to sync decided by range: to is higher than from",
			zap.Uint64("from", uint64(from)),
			zap.Uint64("to", uint64(to)))
		return
	}

	// TODO: this is a temporary solution to prevent syncing already decided heights.
	// Example: Say we received a decided at height 99, and right after we received a decided at height 100
	// before we could advance the controller's height. This would cause the controller to call SyncDecidedByRange.
	// However, height 99 is already synced, so temporarily we reject such requests here.
	// Note: This isn't ideal because sometimes you do want to sync gaps of 1.
	const minGap = 2
	if to-from < minGap {
		return
	}

	err := n.syncer.SyncDecidedByRange(context.Background(), n.interfaceLogger, mid, from, to, func(msg spectypes.SSVMessage) {
		n.msgRouter.Route(n.interfaceLogger, msg)
	})
	if err != nil {
		n.interfaceLogger.Error("failed to sync decided by range", zap.Error(err))
	}
}

// LastDecided fetches last decided from a random set of peers
func (n *p2pNetwork) LastDecided(logger *zap.Logger, mid spectypes.MessageID) ([]p2pprotocol.SyncResult, error) {
	const (
		minPeers = 3
		waitTime = time.Second * 24
	)
	if !n.isReady() {
		return nil, p2pprotocol.ErrNetworkIsNotReady
	}
	pid, maxPeers := commons.ProtocolID(p2pprotocol.LastDecidedProtocol)
	peers, err := waitSubsetOfPeers(logger, n.getSubsetOfPeers, mid.GetPubKey(), minPeers, maxPeers, waitTime, allPeersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "could not get subset of peers")
	}
	return n.makeSyncRequest(logger, peers, mid, pid, &message.SyncMessage{
		Params: &message.SyncParams{
			Identifier: mid,
		},
		Protocol: message.LastDecidedType,
	})
}

// GetHistory sync the given range from a set of peers that supports history for the given identifier
func (n *p2pNetwork) GetHistory(logger *zap.Logger, mid spectypes.MessageID, from, to specqbft.Height, targets ...string) ([]p2pprotocol.SyncResult, specqbft.Height, error) {
	if from >= to {
		return nil, 0, nil
	}

	if !n.isReady() {
		return nil, 0, p2pprotocol.ErrNetworkIsNotReady
	}
	protocolID, peerCount := commons.ProtocolID(p2pprotocol.DecidedHistoryProtocol)
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
		random, err := n.getSubsetOfPeers(logger, mid.GetPubKey(), peerCount, n.peersWithProtocolsFilter(protocolID))
		if err != nil {
			return nil, 0, errors.Wrap(err, "could not get subset of peers")
		}
		peers = random
	}
	maxBatchRes := specqbft.Height(n.cfg.MaxBatchResponse)

	var results []p2pprotocol.SyncResult
	var err error
	currentEnd := to
	if to-from > maxBatchRes {
		currentEnd = from + maxBatchRes
	}
	results, err = n.makeSyncRequest(logger, peers, mid, protocolID, &message.SyncMessage{
		Params: &message.SyncParams{
			Height:     []specqbft.Height{from, currentEnd},
			Identifier: mid,
		},
		Protocol: message.DecidedHistoryType,
	})
	if err != nil {
		return results, 0, err
	}
	return results, currentEnd, nil
}

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
