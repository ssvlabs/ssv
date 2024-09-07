package discovery

import (
	"sync"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"

	"github.com/ssvlabs/ssv/networkconfig"
)

type forkListener struct {
	netCfg           networkconfig.NetworkConfig
	preForkListener  *discover.UDPv5
	postForkListener *discover.UDPv5
	closeOnce        sync.Once
}

func newForkListener(netCfg networkconfig.NetworkConfig, preFork, postFork *discover.UDPv5) *forkListener {
	return &forkListener{
		netCfg:           netCfg,
		preForkListener:  preFork,
		postForkListener: postFork,
	}
}

func (l *forkListener) Lookup(id enode.ID) []*enode.Node {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.Lookup(id)
	}
	return l.preForkListener.Lookup(id)
}

func (l *forkListener) RandomNodes() enode.Iterator {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.RandomNodes()
	}
	return l.preForkListener.RandomNodes()
}

func (l *forkListener) AllNodes() []*enode.Node {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.AllNodes()
	}
	return l.preForkListener.AllNodes()
}

func (l *forkListener) Ping(node *enode.Node) error {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.Ping(node)
	}
	return l.preForkListener.Ping(node)
}

func (l *forkListener) LocalNode() *enode.LocalNode {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.LocalNode()
	}
	return l.preForkListener.LocalNode()
}

func (l *forkListener) Close() {
	l.closePreForkListener()
	l.postForkListener.Close()
}

// closePreForkListener ensures preForkListener is closed once
func (l *forkListener) closePreForkListener() {
	l.closeOnce.Do(func() {
		l.preForkListener.Close()
	})
}
