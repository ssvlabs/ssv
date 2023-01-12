package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"

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
	"github.com/bloxapp/ssv/utils/tasks"
)

// extremeLowPeerCount is the maximum number of peers considered as too low
// when trying to get a subset of peers for a specific subnet.
const extremelyLowPeerCount = 32

func (n *p2pNetwork) SyncHighestDecided(mid spectypes.MessageID) error {
	go func() {
		logger := n.logger.With(zap.String("identifier", mid.String()))
		lastDecided, err := n.LastDecided(mid)
		if err != nil {
			logger.Debug("highest decided: sync failed", zap.Error(err))
			return
		}
		if len(lastDecided) == 0 {
			logger.Debug("highest decided: no messages were synced")
			return
		}
		results := p2pprotocol.SyncResults(lastDecided)
		results.ForEachSignedMessage(func(m *specqbft.SignedMessage) {
			raw, err := m.Encode()
			if err != nil {
				logger.Warn("could not encode signed message")
				return
			}
			n.msgRouter.Route(spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   mid,
				Data:    raw,
			})
		})
	}()

	return nil
}

func (n *p2pNetwork) SyncDecidedByRange(mid spectypes.MessageID, from, to qbft.Height) {
	if !n.cfg.SyncHistory {
		return
	}

	go func() {
		logger := n.logger.With(
			zap.String("what", "SyncDecidedByRange"),
			zap.String("identifier", mid.String()),
			zap.Uint64("from", uint64(from)),
			zap.Uint64("to", uint64(to)))
		logger.Debug("syncing decided by range")

		err := n.getDecidedByRange(context.Background(), logger, mid, from, to, func(sm *specqbft.SignedMessage) error {
			raw, err := sm.Encode()
			if err != nil {
				logger.Warn("could not encode signed message")
				return nil
			}
			n.msgRouter.Route(spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   mid,
				Data:    raw,
			})
			return nil
		})
		if err != nil {
			logger.Debug("sync failed", zap.Error(err))
		}
	}()
}

func (n *p2pNetwork) getDecidedByRange(ctx context.Context, logger *zap.Logger, mid spectypes.MessageID, from, to specqbft.Height, handler func(*specqbft.SignedMessage) error) error {
	const getHistoryRetries = 2

	var (
		visited = make(map[specqbft.Height]struct{})
		msgs    []p2pprotocol.SyncResult
	)

	tail := from
	var err error
	for tail < to {
		err := tasks.RetryWithContext(ctx, func() error {
			start := time.Now()
			msgs, tail, err = n.GetHistory(mid, tail, to)
			if err != nil {
				return err
			}
			handled := 0
			p2pprotocol.SyncResults(msgs).ForEachSignedMessage(func(m *specqbft.SignedMessage) {
				if ctx.Err() != nil {
					return
				}
				if _, ok := visited[m.Message.Height]; ok {
					return
				}
				if err := handler(m); err != nil {
					logger.Warn("could not handle signed message")
				}
				handled++
				visited[m.Message.Height] = struct{}{}
			})
			logger.Debug("received and processed history batch",
				zap.Int64("tail", int64(tail)),
				zap.Duration("duration", time.Since(start)),
				zap.Int("results_count", len(msgs)),
				// TODO: remove this after testing
				zap.String("results", func() string {
					// Prints msgs as:
					//   "(type=1 height=1 round=1) (type=1 height=2 round=1) ..."
					var s []string
					for _, m := range msgs {
						var sm *specqbft.SignedMessage
						if m.Msg.MsgType == spectypes.SSVConsensusMsgType {
							sm = &specqbft.SignedMessage{}
							if err := sm.Decode(m.Msg.Data); err != nil {
								s = append(s, fmt.Sprintf("(%v)", err))
								continue
							}
							s = append(s, fmt.Sprintf("(type=%d height=%d round=%d)", m.Msg.MsgType, sm.Message.Height, sm.Message.Round))
						}
						s = append(s, fmt.Sprintf("(type=%d)", m.Msg.MsgType))
					}
					return strings.Join(s, ", ")
				}()),
				zap.Int("handled", handled))
			return nil
		}, getHistoryRetries)
		if err != nil {
			return err
		}
	}

	return nil
}

// LastDecided fetches last decided from a random set of peers
func (n *p2pNetwork) LastDecided(mid spectypes.MessageID) ([]p2pprotocol.SyncResult, error) {
	if !n.isReady() {
		return nil, p2pprotocol.ErrNetworkIsNotReady
	}
	pid, peerCount := n.fork.ProtocolID(p2pprotocol.LastDecidedProtocol)
	peers, err := n.getSubsetOfPeers(mid.GetPubKey(), peerCount, allPeersFilter)
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
func (n *p2pNetwork) GetHistory(mid spectypes.MessageID, from, to specqbft.Height, targets ...string) ([]p2pprotocol.SyncResult, specqbft.Height, error) {
	if from >= to {
		return nil, 0, nil
	}

	if !n.isReady() {
		return nil, 0, p2pprotocol.ErrNetworkIsNotReady
	}
	protocolID, peerCount := n.fork.ProtocolID(p2pprotocol.DecidedHistoryProtocol)
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
		random, err := n.getSubsetOfPeers(mid.GetPubKey(), peerCount, n.peersWithProtocolsFilter(string(protocolID)))
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
	results, err = n.makeSyncRequest(peers, mid, protocolID, &message.SyncMessage{
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
func (n *p2pNetwork) RegisterHandlers(handlers ...*p2pprotocol.SyncHandler) {
	m := make(map[libp2p_protocol.ID][]p2pprotocol.RequestHandler)
	for _, handler := range handlers {
		pid, _ := n.fork.ProtocolID(handler.Protocol)
		current, ok := m[pid]
		if !ok {
			current = make([]p2pprotocol.RequestHandler, 0)
		}
		current = append(current, handler.Handler)
		m[pid] = current
	}

	for pid, phandlers := range m {
		n.registerHandlers(pid, phandlers...)
	}
}

func (n *p2pNetwork) registerHandlers(pid libp2p_protocol.ID, handlers ...p2pprotocol.RequestHandler) {
	handler := p2pprotocol.CombineRequestHandlers(handlers...)
	n.host.SetStreamHandler(pid, func(stream libp2pnetwork.Stream) {
		req, respond, done, err := n.streamCtrl.HandleStream(stream)
		defer done()
		if err != nil {
			n.logger.Warn("could not handle stream", zap.Error(err))
			return
		}
		smsg, err := n.fork.DecodeNetworkMsg(req)
		if err != nil {
			n.logger.Warn("could not decode msg from stream", zap.Error(err))
			return
		}
		result, err := handler(smsg)
		if err != nil {
			n.logger.Warn("could not handle msg from stream", zap.Error(err))
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
func (n *p2pNetwork) getSubsetOfPeers(vpk spectypes.ValidatorPK, peerCount int, filter func(peer.ID) bool) (peers []peer.ID, err error) {
	var ps []peer.ID
	seen := make(map[peer.ID]struct{})
	topics := n.fork.ValidatorTopicID(vpk)
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
		// Pubsub's topic/peers association is unreliable when there are few peers.
		// So if we have few peers, we should just filter all of them (regardless of topic.)
		allPeers := n.host.Network().Peers()
		if len(allPeers) <= extremelyLowPeerCount {
			for _, peer := range allPeers {
				if filter(peer) {
					peers = append(peers, peer)
				}
			}
		}

		if len(peers) == 0 {
			n.logger.Debug("could not find peers", zap.Any("topics", topics))
			return nil, nil
		}
	}
	if peerCount > len(peers) {
		peerCount = len(peers)
	} else {
		rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	}
	return peers[:peerCount], nil
}

func (n *p2pNetwork) makeSyncRequest(peers []peer.ID, mid spectypes.MessageID, protocol libp2p_protocol.ID, syncMsg *message.SyncMessage) ([]p2pprotocol.SyncResult, error) {
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
	encoded, err := n.fork.EncodeNetworkMsg(msg)
	if err != nil {
		return nil, err
	}
	logger := n.logger.With(zap.String("protocol", string(protocol)), zap.String("identifier", mid.String()))
	msgID := n.fork.MsgID()
	distinct := make(map[string]struct{})
	for _, pid := range peers {
		logger := logger.With(zap.String("peer", pid.String()))
		raw, err := n.streamCtrl.Request(pid, protocol, encoded)
		if err != nil {
			logger.Debug("could not make stream request", zap.Error(err))
			continue
		}
		mid := msgID(raw)
		if _, ok := distinct[mid]; ok {
			continue
		}
		distinct[mid] = struct{}{}
		res, err := n.fork.DecodeNetworkMsg(raw)
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
