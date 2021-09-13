package ibft

// Reader is an interface for ibft in the context of an exporter
type Reader interface {
	Start() error
}

// Syncer is an interface for syncing data
type Syncer interface {
	Sync() error
}

// SyncRead reads and sync data
type SyncRead interface {
	Reader
	Syncer
}
