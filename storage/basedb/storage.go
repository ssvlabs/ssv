package basedb

import (
	"context"
	"time"
)

// Options for creating all db type
type Options struct {
	Type       string        `yaml:"Type" env:"DB_TYPE" env-default:"badger-db" env-description:"Type of db badger-db or badger-memory"`
	Path       string        `yaml:"Path" env:"DB_PATH" env-default:"./data/db" env-description:"Path for storage"`
	Reporting  bool          `yaml:"Reporting" env:"DB_REPORTING" env-default:"false" env-description:"Flag to run on-off db size reporting"`
	GCInterval time.Duration `yaml:"GCInterval" env:"DB_GC_INTERVAL" env-default:"6m" env-description:"Interval between garbage collection cycles. Set to 0 to disable."`
	Ctx        context.Context
}

// Reader is a read-only accessor to the database.
type Reader interface {
	Get(prefix []byte, key []byte) (Obj, bool, error)
	GetMany(prefix []byte, keys [][]byte, iterator func(Obj) error) error
	GetAll(prefix []byte, handler func(int, Obj) error) error
}

// ReadWrite is a read-write accessor to the database.
// NOTE TO REMOVE: there is no just `Writer` in addition to `ReadWriter` because write transactions always allow for both read & write (at least in Badger)
type ReadWriter interface {
	Reader
	Set(prefix []byte, key []byte, value []byte) error
	SetMany(prefix []byte, n int, next func(int) (Obj, error)) error
	Delete(prefix []byte, key []byte) error
}

// Txn is a read-write transaction.
type Txn interface {
	ReadWriter
	// TODO: add iterator
	Commit() error
	Discard()
}

// Database interface for Badger DB
type Database interface {
	RWTxn() Txn
	ROTxn() Reader // TODO: afaik there is no effect for Commit/Discard on read-only transactions so a `Reader` is sufficient?
	ReadWriter
	// TODO: consider moving these functions into Reader and ReadWriter interfaces?
	CountByCollection(prefix []byte) (int64, error)
	DeleteByPrefix(prefix []byte) (int, error)
	RemoveAllByCollection(prefix []byte) error
	Update(fn func(Txn) error) error
	Close() error
}

// GarbageCollector is an interface implemented by storage engines which demand garbage collection.
type GarbageCollector interface {
	// QuickGC runs a short garbage collection cycle to reclaim some unused disk space.
	// Designed to be called periodically while the database is being used.
	QuickGC(context.Context) error

	// FullGC runs a long garbage collection cycle to reclaim (ideally) all unused disk space.
	// Designed to be called when the database is not being used.
	FullGC(context.Context) error
}

// Obj struct for getting key/value from storage
type Obj struct {
	Key   []byte
	Value []byte
}
