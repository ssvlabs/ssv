package history

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// getPeersLastMsgs will request the last msgs sent by peers, if any exist for a running instance
func (s *Sync) getPeersLastMsgs() ([]*proto.SignedMessage, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	usedPeers, err := s.getPeers(4)
	if err != nil {
		return nil, errors.Wrap(err, "could not get peers for fetching current instance")
	}

	wg := sync.WaitGroup{}
	res := make([]*proto.SignedMessage, 0)
	for _, p := range usedPeers {
		wg.Add(1)
		go func(peer string) {
			msg, err := s.network.GetCurrentInstance(peer, &network.SyncMessage{
				Type:   network.Sync_GetCurrentInstance,
				Lambda: s.identifier,
			})
			if err != nil {
				s.logger.Error("error fetching current instance", zap.Error(err))
			} else if err := s.currentInstanceResError(msg); err != nil {
				s.logger.Error("error fetching current instance", zap.Error(err))
			} else {
				res = append(res, msg.SignedMessages[0])
			}
			wg.Done()
		}(p)
	}
	wg.Wait()
	return res, nil
}

func (s *Sync) currentInstanceResError(msg *network.SyncMessage) error {
	if msg == nil {
		return errors.New("msg is nil")
	} else if len(msg.Error) > 0 {
		return errors.New("error fetching current instance: " + msg.Error)
	} else if len(msg.SignedMessages) != 1 {
		return errors.New("error fetching current instance, invalid result count")
	}
	return nil
}
