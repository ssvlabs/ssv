package ibft

// Reader is a minimal interface for ibft in the context of an exporter
type Reader interface {
	Start() error
}

// Syncer is a minimal interface for syncing data
type Syncer interface {
	Sync() error
}

type SyncRead interface {
	Reader
	Syncer
}