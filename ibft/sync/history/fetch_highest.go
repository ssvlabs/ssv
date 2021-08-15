package history

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// findHighestInstance returns the highest found decided signed message and the peer it was received from
func (s *Sync) findHighestInstance() (*proto.SignedMessage, string, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	// TODO select peers by quality/ score?
	// TODO - should be changed to support multi duty
	usedPeers, err := s.network.AllPeers(s.publicKey)
	if err != nil {
		return nil, "", err
	}
	if len(usedPeers) > 4 {
		usedPeers = usedPeers[:4]
	}

	results := s.getHighestDecidedFromPeers(usedPeers)

	// no decided msgs were received from peers, return error
	if len(results) == 0 {
		s.logger.Debug("could not fetch highest decided from peers",
			zap.String("identifier", hex.EncodeToString(s.identifier)))
		return nil, "", errors.New("could not fetch highest decided from peers")
	}

	// find the highest decided within the incoming messages
	var ret *proto.SignedMessage
	var fromPeer string
	for _, res := range results {
		if res.Error == kv.EntryNotFoundError {
			continue
		}

		if ret == nil {
			ret = res.SignedMessages[0]
			fromPeer = res.FromPeerID
		}
		if ret.Message.SeqNumber < res.SignedMessages[0].Message.SeqNumber {
			ret = res.SignedMessages[0]
			fromPeer = res.FromPeerID
		}
	}

	// highest decided is a nil msg, meaning no decided found from peers. This can happen if no previous decided instance exists.
	if ret == nil {
		return nil, "", nil
	}

	// found a valid highest decided
	return ret, fromPeer, nil
}
