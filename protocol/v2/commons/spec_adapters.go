package commons

import (
	"fmt"
	qbft2 "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	protcolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"time"
)

func newNetworkAdapter(net protcolp2p.Network) *networkAdapter {
	return &networkAdapter{broadcaster: net, syncer: net, results: cache.New(time.Minute*10, time.Minute*12)}
}

func NewSSVNetworkAdapter(net protcolp2p.Network) ssv.Network {
	return newNetworkAdapter(net)
}

func NewQBFTNetworkAdapter(net protcolp2p.Network) qbft2.Network {
	return newNetworkAdapter(net)
}

type networkAdapter struct {
	broadcaster protcolp2p.Broadcaster
	syncer protcolp2p.Syncer

	results *cache.Cache
}

type SyncResults []protcolp2p.SyncResult

func (na *networkAdapter) SyncHighestDecided(identifier []byte) error {
	mid := types.MessageID{}
	copy(mid[:], identifier)
	res, err := na.syncer.LastDecided(mid)
	if err != nil {
		return err
	}

	k := fmt.Sprintf("sync-%x", identifier)
	if _, ok := na.results.Get(k); !ok {
		na.results.SetDefault(k, res)
	}
	return nil
}

func (na *networkAdapter) SyncHighestRoundChange(identifier []byte, height qbft2.Height) error {
	mid := types.MessageID{}
	copy(mid[:], identifier)

	res, err := na.syncer.LastChangeRound(mid, height)
	if err != nil {
		return err
	}

	k := fmt.Sprintf("sync-cr-%x", identifier)
	if _, ok := na.results.Get(k); !ok {
		na.results.SetDefault(k, res)
	}
	return nil
}

func (na *networkAdapter) Broadcast(msg types.Encoder) error {
	m, ok := msg.(*types.SSVMessage)
	if !ok {
		return errors.New("invalid message structure")
	}
	if m == nil {
		return errors.New("empty message")
	}
	return na.broadcaster.Broadcast(*m)
}

func (na *networkAdapter) BroadcastDecided(msg types.Encoder) error {
	return na.Broadcast(msg)
}

func (na *networkAdapter) GetDecidedResults(identifier []byte) SyncResults {
	return na.getResults("sync", identifier)
}

func (na *networkAdapter) GetChangeRoundResults(identifier []byte) SyncResults {
	return na.getResults("sync-cr", identifier)
}

func (na *networkAdapter) getResults(prefix string, identifier []byte) SyncResults {
	k := fmt.Sprintf("%s-%x", prefix, identifier)
	res, ok := na.results.Get(k)
	if !ok {
		return nil
	}
	results, ok := res.(SyncResults)
	if !ok {
		return nil
	}
	return results
}

func NewQBFTStorageAdapter(store qbftstorage.QBFTStore) qbft2.Storage {
	return &storageAdapter{store: store}
}

type storageAdapter struct {
	store qbftstorage.QBFTStore
}
// SaveHighestDecided saves (and potentially overrides) the highest Decided for a specific instance
func (sa *storageAdapter) SaveHighestDecided(signedMsg *qbft2.SignedMessage) error {
	return sa.store.SaveLastDecided(signedMsg)
}

// GetHighestDecided returns highest decided if found, nil if didn't
func (sa *storageAdapter) GetHighestDecided(identifier []byte) (*qbft2.SignedMessage, error) {
	return sa.store.GetLastDecided(identifier)
}
