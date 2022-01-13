package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
)

// filterIrrelevant is a ConnectionFilter that filters irrelevant operators in case the node reached peer limit
func (n *p2pNetwork) filterIrrelevant(conn libp2pnetwork.Conn) (bool, error) {
	if !n.isPeerAtLimit(conn.Stat().Direction) {
		return true, nil
	}
	id := conn.RemotePeer()
	if !n.peersIndex.Indexed(conn.RemotePeer()) {
		n.trace("connection was not indexed")
		// TODO: filter out in the future, currently might be an old peer or bootnode
		return true, nil
	}
	if relevant, oid := n.isRelevantPeer(id); !relevant {
		n.peersIndex.Prune(id, oid)
		return false, nil
	}
	return true, nil
}

// filterIrrelevant is a ConnectionFilter that filters non ssv nodes, based on User Agent
func (n *p2pNetwork) filterNonSsvNodes(conn libp2pnetwork.Conn) (bool, error) {
	id := conn.RemotePeer()
	ua, err := n.peersIndex.getUserAgent(id)
	if err != nil {
		n.trace("WARNING: could not read user agent", zap.String("peerID", id.String()))
		return true, nil
	}
	if ua.IsUnknown() {
		n.trace("filtering unknown node", zap.String("ua", string(ua)))
		return false, nil
	}
	return true, nil
}

//// filterOldVersion is a ConnectionFilter that filters peers with old version
//	// TODO:
//	// 1. import github.com/hashicorp/go-version
//	// 2. var minSupportedVersion = version.NewVersion(<min version>)
//func (n *p2pNetwork) filterOldVersion(conn libp2pnetwork.Conn) (bool, error) {
//	id := conn.RemotePeer()
//	ua, err := n.peersIndex.getUserAgent(id)
//	if err != nil {
//		return false, err
//	}
//  vstr := ua.NodeVersion()
//	v, err := version.NewVersion(vstr[1:])
//	if err != nil {
//		return false, err
//	}
//	return v.LessThan(minSupportedVersion), nil
//}

// isRelevantPeer checks if the current node should connect the given peer and returns operator id if found.
// a peer is relevant if it fullfils one of the following:
// - it shares a committee with the current node
// - it is an exporter or bootnode (TODO: bootnode)
func (n *p2pNetwork) isRelevantPeer(id peer.ID) (bool, string) {
	where := zap.String("where", "isRelevantPeer()")
	fieldPid := zap.String("peerID", id.String())
	//if !n.peersIndex.Indexed(id) {
	//	logger.Debug("peer was not indexed yet")
	//	return false
	//}
	oid, err := n.peersIndex.getOperatorID(id)
	if err != nil {
		n.trace("WARNING: could not read operator id", where, fieldPid, zap.Error(err))
		return false, ""
	}
	if len(oid) > 0 {
		relevant := n.lookupOperator(oid)
		if !relevant {
			n.trace("operator is not relevant", where, zap.String("oid", oid), fieldPid)
		} else {
			n.trace("operator is relevant", where, zap.String("oid", oid), fieldPid)
		}
		return relevant, oid
	}
	n.trace("could not find operator id, looking for node type", where, fieldPid)
	nodeType, err := n.peersIndex.getNodeType(id)
	if err != nil {
		n.trace("WARNING: could not read node type", where, fieldPid, zap.Error(err))
		return false, ""
	}
	if nodeType == Operator {
		n.trace("WARNING: operator doesn't have an id", where, fieldPid)
		return false, ""
	}
	// TODO: unmark once bootnode enr will include a type as well
	//if nodeType == Unknown {
	//	n.trace("WARNING: unknown peer", where, fieldPid)
	//	return false, ""
	//}
	return true, ""
}

// isPeerAtLimit checks for max peers
func (n *p2pNetwork) isPeerAtLimit(direction libp2pnetwork.Direction) bool {
	numOfConns := len(n.host.Network().Peers())
	// TODO: add a buffer
	return numOfConns >= n.maxPeers
}
