package peers

import (
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"io"
)

// Transactional is an interface for transactional access to a peerstore object
type Transactional interface {
	Commit() error
	Rollback() error
	Get(key string) (interface{}, error)
	Put(key string, val interface{})

	io.Closer
}

type transactional struct {
	pid   peer.ID
	store peerstore.PeerMetadata

	data map[string]interface{}
	orig map[string]interface{}
}

// newTransactional instantiate a Transactional object on top of the given peer.ID and peerstore
func newTransactional(pid peer.ID, store peerstore.PeerMetadata) Transactional {
	return &transactional{
		pid:   pid,
		store: store,
		data:  make(map[string]interface{}),
	}
}

// Commit finalizes the transaction, returns an error to be handled be caller
// which MUST invoke Rollback accordingly
func (t *transactional) Commit() error {
	var err error
	var data interface{}
	t.orig = make(map[string]interface{})
	for k, d := range t.data {
		data, err = t.store.Get(t.pid, k)
		if err != nil {
			break
		}
		t.orig[k] = data
		err = t.store.Put(t.pid, k, d)
		if err != nil {
			break
		}
	}
	return nil
}

// Rollback reverts all the changes that have been made to the object
func (t *transactional) Rollback() error {
	if len(t.orig) == 0 {
		// nothing to rollback
		return nil
	}
	for k, d := range t.orig {
		if err := t.store.Put(t.pid, k, d); err != nil {
			return err
		}
	}
	t.orig = nil
	return nil
}

// Get returns the value of the given key
func (t *transactional) Get(key string) (interface{}, error) {
	if res, ok := t.data[key]; ok {
		return res, nil
	}
	res, err := t.store.Get(t.pid, key)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Put set the value for the given key
func (t *transactional) Put(key string, val interface{}) {
	t.data[key] = val
}

// Close close the transactional
func (t *transactional) Close() error {
	t.orig = nil
	t.data = nil
	return nil
}
