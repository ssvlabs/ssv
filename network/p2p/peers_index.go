package p2p

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	// UserAgentKey is the key for storing to the user agent value
	// NOTE: this value is set by libp2p
	UserAgentKey = "AgentVersion"
	// NodeRecordKey is a key for node record (ENR) value
	NodeRecordKey = "NodeRecord"
	// OperatorIDKey is a key for operator pub-key hash value
	OperatorIDKey = "OperatorID"
	// NodeTypeKey is a key for node type (operator | exporter) value
	NodeTypeKey = "NodeType"
)

// IndexData is the type of stored data
type IndexData map[string]string

// PeersIndex is responsible for storing and serving peers information.
//
// It uses libp2p's Peerstore (github.com/libp2p/go-libp2p-peerstore) to store metadata of peers:
//   - Node Record (ENR)
//   - User Agent - stored by libp2p but accessed through peer index
//   - Operator ID (hash) - derived from ENR
//   - Node Type - derived from ENR
//
// NOTE: Peerstore access could potentially cause intesive CPU work for encoding/decoding keys
//
type PeersIndex interface {
	Run()
	GetData(pid peer.ID, key string) (interface{}, bool, error)
	IndexConn(conn network.Conn)
	IndexNode(node *enode.Node)
	Indexed(id peer.ID) bool
	EvictPruned(oid string)
	Prune(id peer.ID, oid string)
	Pruned(id peer.ID) bool

	exist(id peer.ID, k string) bool
	getUserAgent(id peer.ID) (UserAgent, error)
	getOperatorID(id peer.ID) (string, error)
	getNodeType(id peer.ID) (NodeType, error)
}

// peersIndex implements PeersIndex
type peersIndex struct {
	logger *zap.Logger

	host host.Host
	ids  *identify.IDService

	prunedLock      *sync.RWMutex
	prunedPeers     *cache.Cache
	prunedOperators map[string]string
}

// NewPeersIndex creates a new instance
func NewPeersIndex(logger *zap.Logger, host host.Host, ids *identify.IDService) PeersIndex {
	logger = logger.With(zap.String("who", "PeersIndex"))
	pi := peersIndex{
		logger:          logger,
		host:            host,
		ids:             ids,
		prunedLock:      &sync.RWMutex{},
		prunedPeers:     cache.New(time.Minute*5, time.Minute*6),
		prunedOperators: make(map[string]string),
	}
	// register on eviction of pruned peer
	pi.prunedPeers.OnEvicted(pi.onPrunedPeerEvicted)

	return &pi
}

// Run tries to index data on all available peers
func (pi *peersIndex) Run() {
	if pi.ids == nil {
		return
	}

	conns := pi.host.Network().Conns()
	for _, conn := range conns {
		if err := pi.indexPeerConnection(conn); err != nil {
			pi.logger.Warn("failed to index connection", zap.Error(err))
		}
	}
}

// GetData returns data of the given peer and key
func (pi *peersIndex) GetData(pid peer.ID, key string) (interface{}, bool, error) {
	data, err := pi.host.Peerstore().Get(pid, key)
	if err != nil {
		if err == peerstore.ErrNotFound {
			return "", false, nil
		}
		return "", true, errors.Wrap(err, "could not read data from peerstore")
	}
	return data, true, nil
}

// IndexConn indexes the given peer / connection
func (pi *peersIndex) IndexConn(conn network.Conn) {
	pid := conn.RemotePeer().String()
	// skip if no id service was configured (user agent will be missing)
	if pi.ids == nil {
		return
	}
	if err := pi.indexPeerConnection(conn); err != nil {
		pi.logger.Warn("could not index connection", zap.Error(err),
			zap.String("peerID", pid),
			zap.String("multiaddr", conn.RemoteMultiaddr().String()))
		return
	}
}

// IndexNode indexes the given node
func (pi *peersIndex) IndexNode(node *enode.Node) {
	if err := pi.indexNode(node); err != nil {
		pi.logger.Warn("could not index node", zap.Error(err),
			zap.String("enr", node.String()))
		return
	}
}

// EvictPruned removes the given operator from the pruned peers collection
func (pi *peersIndex) EvictPruned(oid string) {
	pi.prunedLock.Lock()
	defer pi.prunedLock.Unlock()

	if pid, ok := pi.prunedOperators[oid]; ok {
		pi.prunedPeers.Delete(pid)
	}
}

// Prune prunes the given peer
func (pi *peersIndex) Prune(id peer.ID, oid string) {
	pi.prunedLock.Lock()
	defer pi.prunedLock.Unlock()

	pid := id.String()
	pi.prunedPeers.SetDefault(pid, oid)
	pi.prunedOperators[oid] = pid
}

// Pruned returns whether the given peer was pruned
func (pi *peersIndex) Pruned(id peer.ID) bool {
	pi.prunedLock.Lock()
	defer pi.prunedLock.Unlock()

	_, exist := pi.prunedPeers.Get(id.String())
	return exist
}

// Indexed checks if the given peer was indexed
func (pi *peersIndex) Indexed(id peer.ID) bool {
	return pi.exist(id, NodeTypeKey)
}

// exist checks if the given peer/key exist
func (pi *peersIndex) exist(id peer.ID, k string) bool {
	_, err := pi.host.Peerstore().Get(id, k)
	return err == nil || err != peerstore.ErrNotFound
}

// indexPeerConnection (unsafe) indexes the given peer / connection
func (pi *peersIndex) indexPeerConnection(conn network.Conn) error {
	peerID := conn.RemotePeer()
	logger := pi.logger.With(zap.String("peerdID", peerID.String()))
	if pi.Indexed(peerID) {
		//logger.Debug("peer was already indexed")
		return nil
	}
	// force identify protocol
	if !pi.exist(peerID, UserAgentKey) {
		//logger.Debug("start identify")
		pi.ids.IdentifyConn(conn)
		//logger.Debug("done identify")
	}
	ua, err := pi.getUserAgent(peerID)
	if err != nil {
		logger.Warn("could not get user agent", zap.Error(err))
		return err
	}
	if len(string(ua)) == 0 {
		//logger.Warn("could not find user agent")
		return nil
	}
	if ua.IsUnknown() {
		logger.Warn("unknown peer", zap.String("ua", string(ua)))
		return nil
	}
	oid := ua.OperatorID()
	if len(oid) > 0 {
		if err := pi.host.Peerstore().Put(peerID, OperatorIDKey, oid); err != nil {
			return errors.Wrap(err, "could not save operator id")
		}
	}
	nodeType := ua.NodeType()
	if len(nodeType) > 0 && nodeType != Unknown.String() {
		if err := pi.host.Peerstore().Put(peerID, NodeTypeKey, nodeType); err != nil {
			return errors.Wrap(err, "could not save node type")
		}
	}
	logger.Debug("indexed connection", zap.String("nodeType", nodeType),
		zap.Any("operatorID", oid), zap.String("ua", string(ua)))
	return nil
}

// indexNode (unsafe) indexes the given node
func (pi *peersIndex) indexNode(node *enode.Node) error {
	info, err := convertToAddrInfo(node)
	if err != nil || info == nil {
		return errors.Wrap(err, "could not convert node to peer info")
	}
	if pi.Indexed(info.ID) {
		//logger.Debug("peer was already indexed")
		return nil
	}
	raw, err := node.MarshalText()
	if err != nil || len(raw) == 0 {
		return errors.Wrap(err, "could not marshal node")
	}
	if err := pi.host.Peerstore().Put(info.ID, NodeRecordKey, raw); err != nil {
		return errors.Wrap(err, "could not store node record in peerstore")
	}
	oid, err := extractOperatorIDEntry(node.Record())
	operatorID := ""
	if err == nil && oid != nil {
		operatorID = string(*oid)
		if err := pi.host.Peerstore().Put(info.ID, OperatorIDKey, operatorID); err != nil {
			return errors.Wrap(err, "could not store operator id in peerstore")
		}
	}
	nodeType, err := extractNodeTypeEntry(node.Record())
	if err == nil && nodeType != Unknown {
		if err := pi.host.Peerstore().Put(info.ID, NodeTypeKey, nodeType.String()); err != nil {
			return errors.Wrap(err, "could not store operator id in peerstore")
		}
	}
	pi.logger.Debug("indexed node", zap.String("nodeType", nodeType.String()),
		zap.Any("operatorID", operatorID),
		zap.String("enr", node.String()),
		zap.String("peerdID", info.ID.String()))
	return nil
}

func (pi *peersIndex) getOperatorID(id peer.ID) (string, error) {
	data, found, err := pi.GetData(id, OperatorIDKey)
	if err != nil {
		return "", errors.Wrap(err, "could not read operator id")
	}
	if !found {
		return "", nil
	}
	oid, ok := data.(string)
	if !ok {
		return "", errors.Wrap(err, "could not cast operator id to string")
	}
	return oid, nil
}

func (pi *peersIndex) getNodeType(id peer.ID) (NodeType, error) {
	data, found, err := pi.GetData(id, NodeTypeKey)
	if err != nil {
		return Unknown, errors.Wrap(err, "could not read operator id")
	}
	if !found {
		return Unknown, nil
	}
	nodeTypeStr, ok := data.(string)
	if !ok {
		return Unknown, errors.Wrap(err, "could not cast operator id to string")
	}
	return Unknown.FromString(nodeTypeStr), nil
}

func (pi *peersIndex) getUserAgent(id peer.ID) (UserAgent, error) {
	uaRaw, found, err := pi.GetData(id, UserAgentKey)
	if err != nil {
		return NewUserAgent(""), errors.Wrap(err, "could not read user agent")
	}
	if !found {
		return NewUserAgent(""), nil
	}
	ua, ok := uaRaw.(string)
	if !ok {
		return NewUserAgent(""), errors.Wrap(err, "could not parse user agent")
	}
	return NewUserAgent(ua), nil
}

func (pi *peersIndex) onPrunedPeerEvicted(s string, i interface{}) {
	go func() {
		pi.prunedLock.Lock()
		defer pi.prunedLock.Unlock()

		if oid, ok := i.(string); ok {
			delete(pi.prunedOperators, oid)
		}
	}()
}
