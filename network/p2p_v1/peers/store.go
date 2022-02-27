package peers

import (
	"github.com/libp2p/go-libp2p-core/peer"
)

// Key is a struct holding the possible parts of a key to access peer data
type Key struct {
	ID         peer.ID
}

// NewKey creates a new key for a peer
func NewKey(id peer.ID) Key {
	return Key{ID: id}
}

// Store is an interface for managing and accessing peers data
type Store interface {
	// Index indexes the given peer identity
	Index(node *Identity) (bool, error)
	// Score adds score to the given peer
	Score(k Key, score string, val float64) error
	// GetScore returns the desired score for the given peer
	GetScore(k Key, score string) (float64, error)
	// EvictPruned removes the given operator or peer from pruned list
	EvictPruned(k Key)
	// Prune inserts the given peer into pruned list
	Prune(k Key)
	// Pruned checks if the given peer is pruned
	Pruned(k Key) bool
}

