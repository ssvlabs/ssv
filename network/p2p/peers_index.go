package p2p

import (
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

const (
	libp2pAgentKey = "AgentVersion"
	// UserAgentKey is the key for storing to the user agent value
	UserAgentKey = "user-agent"
)

// IndexData is the type of stored data
type IndexData map[string]string

// PeersIndex is responsible for indexing peers information
// index data is not persisted at the moment
type PeersIndex interface {
	Run()
	GetPeerData(pid, key string) string
	IndexPeer(conn network.Conn)
}

// peersIndex implements PeersIndex
type peersIndex struct {
	logger *zap.Logger

	host host.Host
	ids  *identify.IDService

	index map[string]IndexData
	lock  *sync.RWMutex
}

// NewPeersIndex creates a new instance
func NewPeersIndex(host host.Host, ids *identify.IDService, logger *zap.Logger) PeersIndex {
	pi := peersIndex{
		logger: logger,
		host:   host,
		ids:    ids,
		index:  make(map[string]IndexData),
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

// GetPeerData returns data of the given peer and key
func (pi *peersIndex) GetPeerData(pid, key string) string {
	pi.lock.RLock()
	defer pi.lock.RUnlock()

	data, found := pi.index[pid]
	if !found || len(data) == 0 {
		return ""
	}
	if res, ok := data[key]; ok {
		return res
	}
	return ""
}

// IndexPeer indexes the given peer / connection
func (pi *peersIndex) IndexPeer(conn network.Conn) {
	pi.lock.RLock()
	defer pi.lock.RUnlock()

	if pi.ids == nil {
		return
	}

	if err := pi.indexPeerConnection(conn); err != nil {
		pi.logger.Debug("could not index connection", zap.Error(err),
			zap.String("peerID", conn.RemotePeer().String()),
			zap.String("multiaddr", conn.RemoteMultiaddr().String()))
	}
}

// indexPeerConnection indexes (unsafe) the given peer / connection
func (pi *peersIndex) indexPeerConnection(conn network.Conn) error {
	pid := conn.RemotePeer()
	pi.ids.IdentifyConn(conn)
	avRaw, err := pi.host.Peerstore().Get(pid, libp2pAgentKey)
	if err != nil {
		return errors.Wrap(err, "could not read user agent")
	}
	av, ok := avRaw.(string)
	if !ok {
		return errors.Wrap(err, "could not parse user agent")
	}
	var data IndexData
	data, found := pi.index[pid.String()]
	if !found {
		data = IndexData{}
	}
	data[UserAgentKey] = av
	pi.index[pid.String()] = data
	return nil
}
