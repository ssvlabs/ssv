package p2pv1

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/async"
)

// trustedPeersRedialInterval is how often the node re-dials the trusted peers it isn't connected to.
const trustedPeersRedialInterval = 30 * time.Second

// parseTrustedPeers parses Config.TrustedPeers, each a comma-separated list of multiaddrs ending in a peer ID, into
// trustedPeers, merging the addresses of a peer listed more than once.
func (n *p2pNetwork) parseTrustedPeers() error {
	n.trustedPeers = make(map[peer.ID]peer.AddrInfo)
	for _, addrs := range n.cfg.TrustedPeers {
		for addr := range strings.SplitSeq(addrs, ",") {
			addrInfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				return fmt.Errorf("could not parse trusted peer: %w", err)
			}
			trusted := n.trustedPeers[addrInfo.ID]
			trusted.ID = addrInfo.ID
			trusted.Addrs = append(trusted.Addrs, addrInfo.Addrs...)
			n.trustedPeers[addrInfo.ID] = trusted
		}
	}
	return nil
}

// isTrustedPeer reports whether id is one of the node's TrustedPeers.
func (n *p2pNetwork) isTrustedPeer(id peer.ID) bool {
	_, ok := n.trustedPeers[id]
	return ok
}

// untrustedPeers returns peers without the trusted ones.
func (n *p2pNetwork) untrustedPeers(peers []peer.ID) []peer.ID {
	if len(n.trustedPeers) == 0 {
		return peers
	}
	return slices.DeleteFunc(slices.Clone(peers), n.isTrustedPeer)
}

// keepTrustedPeersConnected dials the trusted peers once startDelay has passed, and from then on, every interval,
// re-dials any of them the node isn't connected to, until the network closes.
func (n *p2pNetwork) keepTrustedPeersConnected(startDelay, interval time.Duration) {
	if len(n.trustedPeers) == 0 {
		return
	}
	go func() {
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(startDelay):
		}
		n.dialTrustedPeers()
		async.Interval(n.ctx, interval, n.dialTrustedPeers)
	}()
}

// dialTrustedPeers dials every trusted peer the node isn't connected to, and waits for the dials to end. An address
// that keeps failing isn't re-dialed every time: libp2p backs off dialing it, for up to 5 minutes.
func (n *p2pNetwork) dialTrustedPeers() {
	h := n.Host()
	var wg sync.WaitGroup
	for id, addrInfo := range n.trustedPeers {
		if id == h.ID() || h.Network().Connectedness(id) == p2pnet.Connected {
			continue
		}
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(n.ctx, connectTimeout)
			defer cancel()
			if err := h.Connect(ctx, addrInfo); err != nil {
				n.logger.Debug("could not connect to trusted peer",
					fields.PeerID(id),
					zap.Stringers("addrs", addrInfo.Addrs),
					zap.Error(err))
			}
		})
	}
	wg.Wait()
}
