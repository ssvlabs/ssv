package topics

import (
	"fmt"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/patrickmn/go-cache"
	"time"
)

// MsgMapper is responsible for mapping peers to msgIDs and vise versa
type MsgMapper interface {
	//Add(msg []byte, pi peer.ID)
	Add(msgID string, pi peer.ID)
	GetPeers(msg []byte) []peer.ID
	GetMsgIDs(pi peer.ID) []string
}

// msgMapper implements MsgMapper
type msgMapper struct {
	data *cache.Cache
}

// newMsgIDMapper creates a new MsgMapper
func newMsgIDMapper(ttl time.Duration) MsgMapper {
	return &msgMapper{
		data: cache.New(ttl, ttl+(ttl/5)),
	}
}

// Add adds the pair of msg id and peer id
func (mapper *msgMapper) Add(msgID string, pi peer.ID) {
	piKey := peerIDKey(pi)

	k := fmt.Sprintf("%s/%s", piKey, msgID)

	mapper.data.SetDefault(k, true)
}

// GetPeers returns the peers that are related to the given msg
func (mapper *msgMapper) GetPeers(msg []byte) []peer.ID {
	msgID := SSVMsgID(msg)

	var peerIDs []peer.ID
	items := mapper.data.Items() // TODO: optimize
	for k := range items {
		if k[len(k)-20:] == msgID {
			peerIDs = append(peerIDs, keyToPeerID(k[:len(k)-20]))
		}
	}
	return peerIDs
}

// GetMsgIDs returns the msg ids of the given peer
func (mapper *msgMapper) GetMsgIDs(pi peer.ID) []string {
	pik := peerIDKey(pi)

	var ids []string
	items := mapper.data.Items() // TODO: optimize
	for k := range items {
		if k[:len(k)-20] == pik {
			ids = append(ids, k[len(k)-20:])
		}
	}
	return ids
}

func peerIDKey(pi peer.ID) string {
	return pi.String()
}

func keyToPeerID(k string) peer.ID {
	pid, _ := peer.Decode(k)
	return pid
}
