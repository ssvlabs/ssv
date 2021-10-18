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
}

// peersIndex implements PeersIndex
type peersIndex struct {
	logger *zap.Logger

	host host.Host
	ids  *identify.IDService

	index *sync.Map
}

// NewPeersIndex creates a new instance
func NewPeersIndex(host host.Host, ids *identify.IDService) PeersIndex {
	pi := peersIndex{
		host:  host,
		ids:   ids,
		index: new(sync.Map),
	}

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
			pi.logger.Warn("failed to report peer identity")
		}
	}
}

// GetPeerData returns data of the given peer and key
func (pi *peersIndex) GetPeerData(pid, key string) string {
	loaded, found := pi.index.Load(pid)
	if !found {
		return ""
	}
	data, ok := loaded.(IndexData)
	if !ok {
		return ""
	}
	return data[key]
}

// indexPeerConnection indexes the given peer / connection
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
	loaded, found := pi.index.Load(pid.String())
	if found {
		if data, ok = loaded.(IndexData); !ok {
			pi.logger.Debug("could not parse index data, overriding corrupted value")
			data = IndexData{}
		}
	} else {
		data = IndexData{}
	}
	data[UserAgentKey] = av
	pi.index.Store(pid.String(), data)
	return nil
}
