package ekm

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ReadTxn is a read-only transactional handle supplied by the embedding application.
// Implementations should allow nil to indicate non-transactional access.
// When ekmadapter.NewDatabaseAdapter is used in the node, callers must pass a
// transaction value that is also compatible with basedb.Reader.
type ReadTxn interface {
	Discard()
}

// ReadWriteTxn is a read-write transactional handle supplied by the embedding application.
// Implementations should allow nil to indicate non-transactional access.
// When ekmadapter.NewDatabaseAdapter is used in the node, callers must pass a
// transaction value that is also compatible with basedb.ReadWriter.
type ReadWriteTxn interface {
	ReadTxn
	Commit() error
}

// Obj represents a key/value pair returned by Database reads.
type Obj struct {
	Key   []byte
	Value []byte
}

// Database abstracts the key-value storage operations required by ekm.
// Implementations are responsible for interpreting txn (if non-nil).
type Database interface {
	Get(txn ReadTxn, prefix []byte, key []byte) (Obj, bool, error)
	GetAll(txn ReadTxn, prefix []byte, handler func(int, Obj) error) error
	Set(txn ReadWriteTxn, prefix []byte, key []byte, value []byte) error
	Delete(txn ReadWriteTxn, prefix []byte, key []byte) error
	DropPrefix(prefix []byte) error
}

// BeaconNetwork is the subset of beacon-network behavior required by ekm.
type BeaconNetwork interface {
	NetworkName() string
	GenesisRoot() phase0.Root
	EstimatedCurrentSlot() phase0.Slot
	EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch
	EstimatedSlotAtTime(time time.Time) phase0.Slot
	BeaconForkAtEpoch(epoch phase0.Epoch) (spec.DataVersion, *phase0.Fork)
	// ForkAtVersion returns the configured fork for a data version, and whether it exists.
	// Used to pin a fixed fork (Capella for exits per EIP-7044, genesis for registrations)
	// independently of the current fork.
	ForkAtVersion(version spec.DataVersion) (phase0.Fork, bool)
}
