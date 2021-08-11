package history

import (
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"sync"
)

func (s *Sync) getCurrentInstance() error {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	usedPeers, err := s.getPeers(4)
	if err != nil {
		return errors.Wrap(err, "could not get peers for fetching current instance")
	}

	wg := sync.WaitGroup{}
	for _, p := range usedPeers {
		wg.Add(1)
		go func() {
			s.network.GetCurrentInstance(p, &network.SyncMessage{
				Type:   network.Sync_GetCurrentInstance,
				Lambda: s.identifier,
			})
			wg.Done()
		}()
	}
	wg.Done()
	return nil
}
