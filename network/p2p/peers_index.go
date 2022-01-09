package p2p

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
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

	exist(id peer.ID, k string) bool
	getOperatorID(id peer.ID) (string, error)
	getNodeType(id peer.ID) (NodeType, error)
}

// peersIndex implements PeersIndex
type peersIndex struct {
	logger *zap.Logger

	host host.Host
	ids  *identify.IDService

	lock *sync.RWMutex
}

// NewPeersIndex creates a new instance
func NewPeersIndex(host host.Host, ids *identify.IDService, logger *zap.Logger) PeersIndex {
	logger = logger.With(zap.String("who", "PeersIndex"))
	pi := peersIndex{
		logger: logger,
		host:   host,
		ids:    ids,
		lock:   &sync.RWMutex{},
	}

	return &pi
}

// Run tries to index data on all available peers
func (pi *peersIndex) Run() {
	pi.lock.Lock()
	defer pi.lock.Unlock()

	if pi.ids == nil {
		return
	}

	conns := pi.host.Network().Conns()
	for _, conn := range conns {
		if err := pi.indexPeerConnection(conn); err != nil {
			pi.logger.Warn("failed to report peer identity")
		}
	}
}

// GetData returns data of the given peer and key
func (pi *peersIndex) GetData(pid peer.ID, key string) (interface{}, bool, error) {
	pi.lock.RLock()
	defer pi.lock.RUnlock()

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
	pi.lock.Lock()
	defer pi.lock.Unlock()

	// skip if no id service was configured (user agent will be missing)
	if pi.ids == nil {
		return
	}

	if err := pi.indexPeerConnection(conn); err != nil {
		pi.logger.Warn("could not index connection", zap.Error(err),
			zap.String("peerID", conn.RemotePeer().String()),
			zap.String("multiaddr", conn.RemoteMultiaddr().String()))
	}
}

// IndexNode indexes the given node
func (pi *peersIndex) IndexNode(node *enode.Node) {
	pi.lock.Lock()
	defer pi.lock.Unlock()

	if err := pi.indexNode(node); err != nil {
		pi.logger.Warn("could not index node", zap.Error(err),
			zap.String("enr", node.String()))
	}
}

//// Prune prunes the given peer from the index
//func (pi *peersIndex) Prune(pid string) {
//	pi.lock.Lock()
//	defer pi.lock.Unlock()
//
//	pi.data.Delete(peerIndexKey(pid, UserAgentKey))
//	pi.data.Delete(peerIndexKey(pid, NodeRecordKey))
//	pi.data.Delete(peerIndexKey(pid, OperatorIDKey))
//}

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
	if pi.Indexed(peerID) {
		pi.logger.Debug("peer was already indexed", zap.String("pid", peerID.String()))
		return nil
	}
	// if not visited yet by IDService -> do so now
	if !pi.exist(peerID, UserAgentKey) {
		pi.ids.IdentifyConn(conn)
	}
	ua, err := pi.getUserAgent(peerID)
	if err != nil {
		return err
	}
	if oid := ua.OperatorID(); len(oid) > 0 {
		pi.logger.Debug("operator id was extracted from UserAgent", zap.String("oid", oid))
		if err := pi.host.Peerstore().Put(peerID, OperatorIDKey, oid); err != nil {
			return errors.Wrap(err, "could not save operator id")
		}
	}
	if nodeType := ua.NodeType(); len(nodeType) > 0 {
		if err := pi.host.Peerstore().Put(peerID, NodeTypeKey, nodeType); err != nil {
			return errors.Wrap(err, "could not save node type")
		}
	}
	pi.logger.Debug("indexed connection", zap.String("peerID", peerID.String()),
		zap.String("multiaddr", conn.RemoteMultiaddr().String()),
		zap.String(UserAgentKey, string(ua)))
	return nil
}

// indexNode (unsafe) indexes the given node
func (pi *peersIndex) indexNode(node *enode.Node) error {
	info, err := convertToAddrInfo(node)
	if err != nil || info == nil {
		return errors.Wrap(err, "could not convert node to peer info")
	}
	if pi.Indexed(info.ID) {
		pi.logger.Debug("peer was already indexed", zap.String("pid", info.ID.String()))
		return nil
	}
	raw, err := node.MarshalText()
	if err != nil || len(raw) == 0 {
		return errors.Wrap(err, "could not marshal node")
	}
	if err := pi.host.Peerstore().Put(info.ID, NodeRecordKey, raw); err != nil {
		return errors.Wrap(err, "could not store node record in peerstore")
	}
	if oid, err := extractOperatorIDEntry(node.Record()); err == nil && oid != nil {
		pi.logger.Debug("oid was extracted from ENR", zap.Any("oid", *oid))
		if err := pi.host.Peerstore().Put(info.ID, OperatorIDKey, string(*oid)); err != nil {
			return errors.Wrap(err, "could not store operator id in peerstore")
		}
	}
	if nodeType, err := extractNodeTypeEntry(node.Record()); err == nil {
		pi.logger.Debug("nodeType was extracted from ENR", zap.String("nodeType", nodeType.String()))
		if err := pi.host.Peerstore().Put(info.ID, NodeTypeKey, nodeType.String()); err != nil {
			return errors.Wrap(err, "could not store operator id in peerstore")
		}
	}
	pi.logger.Debug("indexed node", zap.String("enr", node.String()))
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
	uaRaw, err := pi.host.Peerstore().Get(id, UserAgentKey)
	if err != nil {
		return NewUserAgent(""), errors.Wrap(err, "could not read user agent")
	}
	ua, ok := uaRaw.(string)
	if !ok {
		return NewUserAgent(""), errors.Wrap(err, "could not parse user agent")
	}
	return NewUserAgent(ua), nil
}
