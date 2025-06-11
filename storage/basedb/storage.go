package basedb

import (
	"context"
	"time"
)

// Options for creating all db type
type Options struct {
	Ctx        context.Context
	Path       string        `yaml:"Path" env:"DB_PATH" env-default:"./data/db" env-description:"Database storage directory path"`
	Reporting  bool          `yaml:"Reporting" env:"DB_REPORTING" env-default:"false" env-description:"Enable database size reporting"`
	GCInterval time.Duration `yaml:"GCInterval" env:"DB_GC_INTERVAL" env-default:"6m" env-description:"Interval between garbage collection runs (0 to disable)"`
}

// Reader is a read-only accessor to the database.
type Reader interface {
	Get(prefix []byte, key []byte) (Obj, bool, error)
	GetMany(prefix []byte, keys [][]byte, iterator func(Obj) error) error
	GetAll(prefix []byte, handler func(int, Obj) error) error
}

// ReadWriter is a read-write accessor to the database.
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

type ReadTxn interface {
	Reader
	Discard()
}

// Database interface for Badger DB
type Database interface {
	ReadWriter

	Begin() Txn
	BeginRead() ReadTxn

	Using(rw ReadWriter) ReadWriter
	UsingReader(r Reader) Reader

	// TODO: consider moving these functions into Reader and ReadWriter interfaces?
	CountPrefix(prefix []byte) (int64, error)
	DropPrefix(prefix []byte) error
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
